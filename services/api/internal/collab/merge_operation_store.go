package collab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

var (
	ErrMergeBusy              = errors.New("merge operation is busy")
	ErrMergeOperationConflict = errors.New("merge operation changed concurrently")
)

// MergeWorkflowStore is deliberately separate from Store so existing
// collaboration fakes and review semantics remain compatible.
type MergeWorkflowStore interface {
	ListSuccessfulCI(ctx context.Context, repositoryID, branch, revision string) (bool, error)
	GetMergeOperation(ctx context.Context, repositoryID string, number int64) (MergeOperation, error)
	RefreshMergeRequestRevisions(ctx context.Context, actor platform.User, repositoryID string, number int64,
		sourceRevision, targetRevision string) (MergeRequest, error)
	AcquireMergeOperation(
		ctx context.Context, actorID, repositoryID string, number int64,
		sourceRevision, targetRevision, owner string, lease time.Duration,
	) (MergeOperation, error)
	RecordMergeResolutions(ctx context.Context, actor platform.User, operation MergeOperation,
		paths []string, strategy string) (MergeOperation, error)
	UpdateMergeOperation(ctx context.Context, operation MergeOperation) (MergeOperation, error)
	UpdateMergeOperationOwned(ctx context.Context, operation MergeOperation, leaseOwner string) (MergeOperation, error)
	RestartMergeOperation(ctx context.Context, actor platform.User, repositoryID string, number int64,
		sourceRevision, targetRevision, owner string, lease time.Duration) (MergeOperation, error)
	FinalizeMerged(ctx context.Context, actor platform.User, repositoryID string, number int64,
		operationID, pushedRevision string) (MergeRequest, error)
}

func (s *store) ListSuccessfulCI(
	ctx context.Context,
	repositoryID string,
	branch string,
	revision string,
) (bool, error) {
	var success bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM ci_runs
			WHERE repository_id = $1 AND branch = $2 AND revision = $3
			  AND status = 'completed' AND conclusion = 'success'
		)
	`, repositoryID, branch, revision).Scan(&success)
	if err != nil {
		return false, fmt.Errorf("check successful CI: %w", err)
	}
	return success, nil
}

func (s *store) GetMergeOperation(
	ctx context.Context,
	repositoryID string,
	number int64,
) (MergeOperation, error) {
	row := s.pool.QueryRow(ctx, mergeOperationQuery+`
		WHERE mo.repository_id = $1 AND mr.number = $2
	`, repositoryID, number)
	operation, err := scanMergeOperation(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return MergeOperation{}, platform.ErrNotFound
	}
	if err != nil {
		return MergeOperation{}, fmt.Errorf("get merge operation: %w", err)
	}
	operation.Resolutions, err = loadMergeResolutions(ctx, s.pool, operation.ID)
	if err != nil {
		return MergeOperation{}, err
	}
	return operation, nil
}

func (s *store) RecordMergeResolutions(
	ctx context.Context,
	actor platform.User,
	operation MergeOperation,
	paths []string,
	strategy string,
) (MergeOperation, error) {
	if operation.ID == "" || len(paths) == 0 || (strategy != "mine" && strategy != "theirs") {
		return MergeOperation{}, ErrMergeOperationConflict
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return MergeOperation{}, fmt.Errorf("begin merge resolution: %w", err)
	}
	defer rollback(ctx, tx)
	current, err := scanMergeOperation(tx.QueryRow(ctx, mergeOperationQuery+`
		WHERE mo.id = $1
		FOR UPDATE
	`, operation.ID))
	if err != nil {
		return MergeOperation{}, fmt.Errorf("lock merge resolution operation: %w", err)
	}
	current.Resolutions, err = loadMergeResolutions(ctx, tx, current.ID)
	if err != nil {
		return MergeOperation{}, err
	}
	if current.Version != operation.Version || current.LeaseOwner == "" || current.LeaseOwner != operation.LeaseOwner ||
		current.LeaseExpiresAt == nil || !current.LeaseExpiresAt.After(nowUTC()) {
		return MergeOperation{}, ErrMergeOperationConflict
	}
	for _, path := range paths {
		if path == "" || len(path) > 2048 {
			return MergeOperation{}, ErrMergeOperationConflict
		}
		if _, err := tx.Exec(ctx, `
		INSERT INTO merge_operation_resolutions (operation_id, path, strategy, actor_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (operation_id, path) DO UPDATE
		SET strategy = EXCLUDED.strategy, actor_id = EXCLUDED.actor_id, updated_at = now()
		`, operation.ID, path, strategy, actor.ID); err != nil {
			return MergeOperation{}, fmt.Errorf("persist merge resolution: %w", err)
		}
	}
	updated, err := scanMergeOperation(tx.QueryRow(ctx, `
		UPDATE merge_operations
		SET state = CASE WHEN state = 'started' THEN 'conflicts' ELSE state END,
			error_code = NULL, error_detail = NULL, lease_expires_at = now() + interval '5 minutes',
			version = version + 1, updated_at = now(),
			push_authorized_revision = NULL, push_authorized_actor_id = NULL,
			push_authorized_target_branch_id = NULL, push_authorized_at = NULL
		WHERE id = $1 AND version = $2
		RETURNING id, merge_request_id, repository_id, actor_id,
			source_revision, target_revision, staged_revision, pushed_revision, parent_revisions,
			state, conflict_paths, error_code, error_detail, lease_owner,
			lease_expires_at, version, started_at, completed_at, created_at, updated_at
	`, operation.ID, operation.Version))
	if err != nil {
		return MergeOperation{}, fmt.Errorf("update merge resolution operation: %w", err)
	}
	updated.Resolutions, err = loadMergeResolutions(ctx, tx, updated.ID)
	if err != nil {
		return MergeOperation{}, err
	}
	var organizationID string
	if err := tx.QueryRow(ctx, `SELECT organization_id FROM repositories WHERE id = $1`, updated.RepositoryID).
		Scan(&organizationID); err != nil {
		return MergeOperation{}, fmt.Errorf("find merge organization: %w", err)
	}
	if err := insertAudit(ctx, tx, actor.ID, organizationID, updated.RepositoryID,
		"merge_operation.resolution_recorded", "merge_operation", updated.ID); err != nil {
		return MergeOperation{}, err
	}
	if err := insertOutbox(ctx, tx, "merge_operation.resolution_recorded", updated.ID+":"+uuidArg(),
		updated); err != nil {
		return MergeOperation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MergeOperation{}, fmt.Errorf("commit merge resolution: %w", err)
	}
	return updated, nil
}

func (s *store) RefreshMergeRequestRevisions(
	ctx context.Context,
	actor platform.User,
	repositoryID string,
	number int64,
	sourceRevision string,
	targetRevision string,
) (MergeRequest, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return MergeRequest{}, fmt.Errorf("begin merge revision refresh: %w", err)
	}
	defer rollback(ctx, tx)
	mr, err := scanMergeRequestByTxForUpdate(ctx, tx, repositoryID, number)
	if err != nil {
		return MergeRequest{}, err
	}
	if mr.State == "merged" {
		return MergeRequest{}, platform.ErrConflict
	}
	if mr.SourceRevision == sourceRevision && mr.TargetRevision == targetRevision {
		if err := tx.Commit(ctx); err != nil {
			return MergeRequest{}, fmt.Errorf("commit unchanged merge revision refresh: %w", err)
		}
		return mr, nil
	}
	var organizationID string
	if err := tx.QueryRow(ctx, `SELECT organization_id FROM repositories WHERE id = $1`, repositoryID).
		Scan(&organizationID); err != nil {
		return MergeRequest{}, fmt.Errorf("find merge organization: %w", err)
	}
	now := nowUTC()
	if _, err := tx.Exec(ctx, `
		UPDATE merge_requests
		SET source_revision = $3, target_revision = $4, updated_at = $5
		WHERE repository_id = $1 AND number = $2
	`, repositoryID, number, sourceRevision, targetRevision, now); err != nil {
		return MergeRequest{}, fmt.Errorf("refresh merge request revisions: %w", err)
	}
	mr, err = scanMergeRequestByTx(ctx, tx, repositoryID, number)
	if err != nil {
		return MergeRequest{}, err
	}
	if err := insertAudit(ctx, tx, actor.ID, organizationID, repositoryID,
		"merge_request.revisions_refresh", "merge_request", mr.ID); err != nil {
		return MergeRequest{}, err
	}
	if err := insertOutbox(ctx, tx, "merge_request.revisions_refreshed", mr.ID+":"+uuidArg(), mr); err != nil {
		return MergeRequest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MergeRequest{}, fmt.Errorf("commit merge revision refresh: %w", err)
	}
	return mr, nil
}

func (s *store) AcquireMergeOperation(
	ctx context.Context,
	actorID string,
	repositoryID string,
	number int64,
	sourceRevision string,
	targetRevision string,
	owner string,
	lease time.Duration,
) (MergeOperation, error) {
	if lease <= 0 || lease > 30*time.Minute {
		lease = 5 * time.Minute
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return MergeOperation{}, fmt.Errorf("begin merge operation: %w", err)
	}
	defer rollback(ctx, tx)

	now := nowUTC()
	leaseUntil := now.Add(lease)
	row := tx.QueryRow(ctx, mergeOperationQuery+`
		WHERE mo.repository_id = $1 AND mr.number = $2
		FOR UPDATE
	`, repositoryID, number)
	operation, lookupErr := scanMergeOperation(row)
	if lookupErr == nil {
		operation.Resolutions, lookupErr = loadMergeResolutions(ctx, tx, operation.ID)
	}
	if errors.Is(lookupErr, pgx.ErrNoRows) {
		operation = MergeOperation{
			ID:             uuidArg(),
			RepositoryID:   repositoryID,
			SourceRevision: sourceRevision,
			TargetRevision: targetRevision,
			State:          "created",
			ConflictPaths:  []string{},
			Version:        0,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		insertRow := tx.QueryRow(ctx, `
			INSERT INTO merge_operations (
				id, merge_request_id, repository_id, actor_id,
				source_revision, target_revision, state, conflict_paths,
				lease_owner, lease_expires_at, created_at, updated_at
			)
			SELECT $1, mr.id, $2, $3, $4, $5, 'created', '[]'::jsonb, $6, $7, $8, $8
			FROM merge_requests mr
			WHERE mr.repository_id = $2 AND mr.number = $9
			ON CONFLICT (merge_request_id) DO NOTHING
		RETURNING id, merge_request_id, repository_id, actor_id,
			source_revision, target_revision, staged_revision, pushed_revision, parent_revisions,
			state, conflict_paths, error_code, error_detail, lease_owner,
			lease_expires_at, version, started_at, completed_at, created_at, updated_at
		`, operation.ID, repositoryID, actorID, sourceRevision, targetRevision,
			owner, leaseUntil, now, number)
		if err := scanMergeOperationInto(insertRow, &operation); err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				return MergeOperation{}, fmt.Errorf("create merge operation: %w", err)
			}
			// Another request may have inserted the unique operation between the
			// initial lookup and this insert. The conflict-free insert lets that
			// request finish; lock and use its durable state below.
			operation, lookupErr = scanMergeOperation(tx.QueryRow(ctx, mergeOperationQuery+`
				WHERE mo.repository_id = $1 AND mr.number = $2
				FOR UPDATE
			`, repositoryID, number))
			if lookupErr == nil {
				operation.Resolutions, lookupErr = loadMergeResolutions(ctx, tx, operation.ID)
			}
			if errors.Is(lookupErr, pgx.ErrNoRows) {
				return MergeOperation{}, platform.ErrNotFound
			}
			if lookupErr != nil {
				return MergeOperation{}, fmt.Errorf("lock concurrent merge operation: %w", lookupErr)
			}
		} else {
			operation.Resolutions, err = loadMergeResolutions(ctx, tx, operation.ID)
			if err != nil {
				return MergeOperation{}, err
			}
			if err := s.recordMergeOperationEvent(ctx, tx, operation, actorID, "merge_operation.created"); err != nil {
				return MergeOperation{}, err
			}
			if err := tx.Commit(ctx); err != nil {
				return MergeOperation{}, fmt.Errorf("commit merge operation: %w", err)
			}
			return operation, nil
		}
	}
	if lookupErr != nil {
		return MergeOperation{}, fmt.Errorf("lock merge operation: %w", lookupErr)
	}
	if operation.State == "merged" {
		if err := tx.Commit(ctx); err != nil {
			return MergeOperation{}, fmt.Errorf("commit merge operation lookup: %w", err)
		}
		return operation, nil
	}
	if operation.State != "pushed" && operation.State != "merged" && operation.LeaseExpiresAt != nil &&
		operation.LeaseExpiresAt.After(now) && operation.LeaseOwner != owner {
		return MergeOperation{}, ErrMergeBusy
	}
	retryFailedStart := operation.State == "created" && operation.ErrorCode != "" &&
		(operation.SourceRevision != sourceRevision || operation.TargetRevision != targetRevision)
	if operation.State != "aborted" && !retryFailedStart &&
		(operation.SourceRevision != sourceRevision || operation.TargetRevision != targetRevision) {
		return MergeOperation{}, ErrMergeOperationConflict
	}
	reset := operation.State == "aborted" || retryFailedStart
	state := operation.State
	if reset {
		state = "created"
	}
	if reset {
		operation.StagedRevision = ""
		operation.PushedRevision = ""
		operation.ParentRevisions = []string{}
		operation.ConflictPaths = []string{}
		operation.ErrorCode = ""
		operation.ErrorDetail = ""
		operation.StartedAt = nil
		operation.CompletedAt = nil
		operation.SourceRevision = sourceRevision
		operation.TargetRevision = targetRevision
	}
	if reset {
		if _, err := tx.Exec(ctx, `
			DELETE FROM merge_operation_resolutions WHERE operation_id = $1
		`, operation.ID); err != nil {
			return MergeOperation{}, fmt.Errorf("clear reset merge resolutions: %w", err)
		}
	}
	if reset {
		if _, err := tx.Exec(ctx, `
			UPDATE merge_operations
			SET push_authorized_revision = NULL, push_authorized_actor_id = NULL,
			    push_authorized_target_branch_id = NULL, push_authorized_at = NULL
			WHERE id = $1
		`, operation.ID); err != nil {
			return MergeOperation{}, fmt.Errorf("clear stale Lore push authorization: %w", err)
		}
	}
	updateRow := tx.QueryRow(ctx, `
		UPDATE merge_operations
		SET actor_id = $2, source_revision = $3, target_revision = $4,
		    parent_revisions = $5, state = $6, conflict_paths = $7,
		    staged_revision = NULLIF($8, ''), pushed_revision = NULLIF($9, ''),
		    error_code = NULLIF($10, ''), error_detail = NULLIF($11, ''), lease_owner = $12,
		    lease_expires_at = $13, version = version + 1, updated_at = $14
		WHERE id = $1
		RETURNING id, merge_request_id, repository_id, actor_id,
			source_revision, target_revision, staged_revision, pushed_revision, parent_revisions,
			state, conflict_paths, error_code, error_detail, lease_owner,
			lease_expires_at, version, started_at, completed_at, created_at, updated_at
	`, operation.ID, actorID, operation.SourceRevision, operation.TargetRevision,
		mustJSON(operation.ParentRevisions), state, mustJSON(operation.ConflictPaths), operation.StagedRevision,
		operation.PushedRevision, operation.ErrorCode, operation.ErrorDetail, owner, leaseUntil, now)
	if err := scanMergeOperationInto(updateRow, &operation); err != nil {
		return MergeOperation{}, fmt.Errorf("renew merge operation: %w", err)
	}
	operation.Resolutions, err = loadMergeResolutions(ctx, tx, operation.ID)
	if err != nil {
		return MergeOperation{}, err
	}
	action := "merge_operation.lease_acquired"
	if reset {
		action = "merge_operation.restarted"
	}
	if err := s.recordMergeOperationEvent(ctx, tx, operation, actorID, action); err != nil {
		return MergeOperation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MergeOperation{}, fmt.Errorf("commit merge operation renewal: %w", err)
	}
	return operation, nil
}

func (s *store) RestartMergeOperation(
	ctx context.Context,
	actor platform.User,
	repositoryID string,
	number int64,
	sourceRevision string,
	targetRevision string,
	owner string,
	lease time.Duration,
) (MergeOperation, error) {
	if sourceRevision == "" || targetRevision == "" || owner == "" {
		return MergeOperation{}, ErrMergeOperationConflict
	}
	if lease <= 0 || lease > 30*time.Minute {
		lease = 5 * time.Minute
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return MergeOperation{}, fmt.Errorf("begin merge restart: %w", err)
	}
	defer rollback(ctx, tx)
	operation, err := scanMergeOperation(tx.QueryRow(ctx, mergeOperationQuery+`
		WHERE mo.repository_id = $1 AND mr.number = $2
		FOR UPDATE
	`, repositoryID, number))
	if errors.Is(err, pgx.ErrNoRows) {
		return MergeOperation{}, platform.ErrNotFound
	}
	if err != nil {
		return MergeOperation{}, fmt.Errorf("lock merge operation for restart: %w", err)
	}
	operation.Resolutions, err = loadMergeResolutions(ctx, tx, operation.ID)
	if err != nil {
		return MergeOperation{}, err
	}
	now := nowUTC()
	if operation.State == "merged" || operation.State == "pushed" || operation.State == "aborted" {
		return MergeOperation{}, ErrMergeOperationConflict
	}
	if operation.State == "pushing" {
		return MergeOperation{}, ErrMergeBusy
	}
	if operation.LeaseExpiresAt != nil && operation.LeaseExpiresAt.After(now) &&
		operation.LeaseOwner != owner {
		return MergeOperation{}, ErrMergeBusy
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM merge_operation_resolutions WHERE operation_id = $1
	`, operation.ID); err != nil {
		return MergeOperation{}, fmt.Errorf("clear restarted merge resolutions: %w", err)
	}
	row := tx.QueryRow(ctx, `
		UPDATE merge_operations
		SET actor_id = $2, source_revision = $3, target_revision = $4,
		    staged_revision = NULL, pushed_revision = NULL, parent_revisions = '[]'::jsonb,
		    state = 'started', conflict_paths = '[]'::jsonb, error_code = NULL,
		    error_detail = NULL, lease_owner = $5, lease_expires_at = $6,
		    push_authorized_revision = NULL, push_authorized_actor_id = NULL,
		    push_authorized_target_branch_id = NULL, push_authorized_at = NULL,
		    started_at = $7, completed_at = NULL, version = version + 1, updated_at = $7
		WHERE id = $1
		RETURNING id, merge_request_id, repository_id, actor_id,
			source_revision, target_revision, staged_revision, pushed_revision, parent_revisions,
			state, conflict_paths, error_code, error_detail, lease_owner,
			lease_expires_at, version, started_at, completed_at, created_at, updated_at
	`, operation.ID, actor.ID, sourceRevision, targetRevision, owner, now.Add(lease), now)
	var updated MergeOperation
	if err := scanMergeOperationInto(row, &updated); err != nil {
		return MergeOperation{}, fmt.Errorf("restart merge operation: %w", err)
	}
	updated.Resolutions, err = loadMergeResolutions(ctx, tx, updated.ID)
	if err != nil {
		return MergeOperation{}, err
	}
	if err := s.recordMergeOperationEvent(ctx, tx, updated, actor.ID, "merge_operation.restarted"); err != nil {
		return MergeOperation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MergeOperation{}, fmt.Errorf("commit merge restart: %w", err)
	}
	return updated, nil
}

func (s *store) UpdateMergeOperation(ctx context.Context, operation MergeOperation) (MergeOperation, error) {
	return s.updateMergeOperation(ctx, operation, "")
}

func (s *store) UpdateMergeOperationOwned(
	ctx context.Context,
	operation MergeOperation,
	leaseOwner string,
) (MergeOperation, error) {
	return s.updateMergeOperation(ctx, operation, leaseOwner)
}

func (s *store) updateMergeOperation(
	ctx context.Context,
	operation MergeOperation,
	expectedLeaseOwner string,
) (MergeOperation, error) {
	if operation.ID == "" {
		return MergeOperation{}, ErrMergeOperationConflict
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return MergeOperation{}, fmt.Errorf("begin merge operation update: %w", err)
	}
	defer rollback(ctx, tx)
	now := nowUTC()
	row := tx.QueryRow(ctx, `
		UPDATE merge_operations
		SET source_revision = $2, target_revision = $3, staged_revision = NULLIF($4, ''),
		    pushed_revision = NULLIF($5, ''), parent_revisions = $6, state = $7, conflict_paths = $8,
		    error_code = NULLIF($9, ''), error_detail = NULLIF($10, ''),
		    lease_owner = NULLIF($11, ''), lease_expires_at = $12,
		    push_authorized_revision = CASE WHEN state = 'pushing' THEN push_authorized_revision ELSE NULL END,
		    push_authorized_actor_id = CASE WHEN state = 'pushing' THEN push_authorized_actor_id ELSE NULL END,
		    push_authorized_target_branch_id = CASE WHEN state = 'pushing'
		        THEN push_authorized_target_branch_id ELSE NULL END,
		    push_authorized_at = CASE WHEN state = 'pushing' THEN push_authorized_at ELSE NULL END,
		    started_at = $13, completed_at = $14, version = version + 1, updated_at = $15
		WHERE id = $1 AND version = $16
		  AND ($17 = '' OR (lease_owner = $17 AND lease_expires_at > $18))
		RETURNING id, merge_request_id, repository_id, actor_id,
			source_revision, target_revision, staged_revision, pushed_revision, parent_revisions,
			state, conflict_paths, error_code, error_detail, lease_owner,
			lease_expires_at, version, started_at, completed_at, created_at, updated_at
	`, operation.ID, operation.SourceRevision, operation.TargetRevision, operation.StagedRevision,
		operation.PushedRevision, mustJSON(operation.ParentRevisions), operation.State,
		mustJSON(operation.ConflictPaths), operation.ErrorCode, operation.ErrorDetail, operation.LeaseOwner,
		operation.LeaseExpiresAt, operation.StartedAt, operation.CompletedAt, now, operation.Version,
		expectedLeaseOwner, now)
	var updated MergeOperation
	err = scanMergeOperationInto(row, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return MergeOperation{}, ErrMergeOperationConflict
	}
	if err != nil {
		return MergeOperation{}, fmt.Errorf("update merge operation: %w", err)
	}
	updated.Resolutions, err = loadMergeResolutions(ctx, tx, updated.ID)
	if err != nil {
		return MergeOperation{}, err
	}
	var organizationID string
	if err := tx.QueryRow(ctx, `SELECT organization_id FROM repositories WHERE id = $1`, updated.RepositoryID).
		Scan(&organizationID); err != nil {
		return MergeOperation{}, fmt.Errorf("find merge organization: %w", err)
	}
	if updated.ActorID != "" {
		if err := insertAudit(ctx, tx, updated.ActorID, organizationID, updated.RepositoryID,
			"merge_operation.updated", "merge_operation", updated.ID); err != nil {
			return MergeOperation{}, err
		}
	}
	if err := insertOutbox(ctx, tx, "merge_operation.updated", updated.ID+":"+uuidArg(), updated); err != nil {
		return MergeOperation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MergeOperation{}, fmt.Errorf("commit merge operation update: %w", err)
	}
	return updated, nil
}

func (s *store) FinalizeMerged(
	ctx context.Context,
	actor platform.User,
	repositoryID string,
	number int64,
	operationID string,
	pushedRevision string,
) (MergeRequest, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return MergeRequest{}, fmt.Errorf("begin merge finalization: %w", err)
	}
	defer rollback(ctx, tx)
	operation, err := scanMergeOperation(tx.QueryRow(ctx, mergeOperationQuery+`
		WHERE mo.id = $1 AND mo.repository_id = $2 AND mr.number = $3
		FOR UPDATE
	`, operationID, repositoryID, number))
	if errors.Is(err, pgx.ErrNoRows) {
		return MergeRequest{}, platform.ErrNotFound
	}
	if err != nil {
		return MergeRequest{}, fmt.Errorf("lock merge operation for finalization: %w", err)
	}
	mr, err := scanMergeRequestByTxForUpdate(ctx, tx, repositoryID, number)
	if err != nil {
		return MergeRequest{}, err
	}
	if mr.State == "merged" {
		finalRevision := pushedRevision
		if mr.MergedRevision != nil {
			if finalRevision != "" && *mr.MergedRevision != finalRevision {
				return MergeRequest{}, platform.ErrConflict
			}
			finalRevision = *mr.MergedRevision
		}
		if operation.PushedRevision != "" && operation.PushedRevision != finalRevision {
			return MergeRequest{}, platform.ErrConflict
		}
		if operation.State != "merged" {
			_, err = tx.Exec(ctx, `
				UPDATE merge_operations
				SET state = 'merged', pushed_revision = COALESCE(pushed_revision, $2),
				    lease_owner = NULL, lease_expires_at = NULL, completed_at = COALESCE(completed_at, now()),
				    version = version + 1, updated_at = now()
				WHERE id = $1
			`, operation.ID, finalRevision)
			if err != nil {
				return MergeRequest{}, fmt.Errorf("reconcile merge operation: %w", err)
			}
			reconciled := operation
			reconciled.State = "merged"
			reconciled.PushedRevision = finalRevision
			reconciled.LeaseOwner = ""
			reconciled.LeaseExpiresAt = nil
			if err := s.recordMergeOperationEvent(ctx, tx, reconciled, actor.ID,
				"merge_operation.reconciled"); err != nil {
				return MergeRequest{}, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return MergeRequest{}, fmt.Errorf("commit merge reconciliation: %w", err)
		}
		return mr, nil
	}
	if operation.State != "pushed" || pushedRevision == "" || operation.PushedRevision != pushedRevision {
		return MergeRequest{}, ErrMergeOperationConflict
	}
	var organizationID string
	if err := tx.QueryRow(ctx, `SELECT organization_id FROM repositories WHERE id = $1`, repositoryID).
		Scan(&organizationID); err != nil {
		return MergeRequest{}, fmt.Errorf("find merge organization: %w", err)
	}
	now := nowUTC()
	if err := tx.QueryRow(ctx, `
		UPDATE merge_requests
		SET state = 'merged', merged_by = $3, merged_revision = $4,
		    merged_at = $5, closed_at = $5, updated_at = $5
		WHERE repository_id = $1 AND number = $2 AND state = 'open' AND NOT is_draft
		RETURNING id
	`, repositoryID, number, actor.ID, pushedRevision, now).Scan(&mr.ID); errors.Is(err, pgx.ErrNoRows) {
		return MergeRequest{}, platform.ErrConflict
	} else if err != nil {
		return MergeRequest{}, fmt.Errorf("finalize merge request: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE merge_operations
		SET state = 'merged', pushed_revision = $2, lease_owner = NULL,
		    lease_expires_at = NULL, completed_at = $3, version = version + 1, updated_at = $3
		WHERE id = $1
	`, operation.ID, pushedRevision, now); err != nil {
		return MergeRequest{}, fmt.Errorf("complete merge operation: %w", err)
	}
	completed := operation
	completed.State = "merged"
	completed.PushedRevision = pushedRevision
	completed.LeaseOwner = ""
	completed.LeaseExpiresAt = nil
	completed.CompletedAt = &now
	if err := s.recordMergeOperationEvent(ctx, tx, completed, actor.ID, "merge_operation.completed"); err != nil {
		return MergeRequest{}, err
	}
	mr, err = scanMergeRequestByTx(ctx, tx, repositoryID, number)
	if err != nil {
		return MergeRequest{}, err
	}
	if err := insertAudit(ctx, tx, actor.ID, organizationID, repositoryID,
		"merge_request.merge", "merge_request", mr.ID); err != nil {
		return MergeRequest{}, err
	}
	if err := insertOutbox(ctx, tx, "merge_request.merged", mr.ID+":"+uuidArg(), mr); err != nil {
		return MergeRequest{}, err
	}
	if err := RecordWorkItemEvent(ctx, tx, WorkItemEventRecord{
		RepositoryID: repositoryID, ItemKind: WorkItemMergeRequest, ItemID: mr.ID,
		ActorID: actor.ID, Kind: EventMerged,
		Payload: WorkItemEventPayload{Revision: pushedRevision},
	}); err != nil {
		return MergeRequest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MergeRequest{}, fmt.Errorf("commit merge finalization: %w", err)
	}
	return mr, nil
}

func (s *store) recordMergeOperationEvent(
	ctx context.Context,
	tx pgx.Tx,
	operation MergeOperation,
	actorID string,
	action string,
) error {
	var organizationID string
	if err := tx.QueryRow(ctx, `
		SELECT organization_id FROM repositories WHERE id = $1
	`, operation.RepositoryID).Scan(&organizationID); err != nil {
		return fmt.Errorf("find merge organization: %w", err)
	}
	if actorID != "" {
		if err := insertAudit(ctx, tx, actorID, organizationID, operation.RepositoryID,
			action, "merge_operation", operation.ID); err != nil {
			return err
		}
	}
	if err := insertOutbox(ctx, tx, action, operation.ID+":"+uuidArg(), operation); err != nil {
		return err
	}
	return nil
}

const mergeOperationQuery = `
	SELECT mo.id, mo.merge_request_id, mo.repository_id, mo.actor_id,
	       mo.source_revision, mo.target_revision, mo.staged_revision, mo.pushed_revision,
	       mo.parent_revisions,
	       mo.state, mo.conflict_paths, mo.error_code, mo.error_detail,
	       mo.lease_owner, mo.lease_expires_at, mo.version, mo.started_at,
	       mo.completed_at, mo.created_at, mo.updated_at
	FROM merge_operations mo
	JOIN merge_requests mr ON mr.id = mo.merge_request_id
`

func scanMergeOperation(row pgx.Row) (MergeOperation, error) {
	var operation MergeOperation
	err := scanMergeOperationInto(row, &operation)
	return operation, err
}

func scanMergeOperationInto(row pgx.Row, operation *MergeOperation) error {
	var actorID, staged, pushed, errorCode, errorDetail, leaseOwner *string
	var conflictJSON, parentJSON []byte
	err := row.Scan(
		&operation.ID, &operation.MergeRequestID, &operation.RepositoryID, &actorID,
		&operation.SourceRevision, &operation.TargetRevision, &staged, &pushed, &parentJSON,
		&operation.State, &conflictJSON, &errorCode, &errorDetail, &leaseOwner,
		&operation.LeaseExpiresAt, &operation.Version, &operation.StartedAt,
		&operation.CompletedAt, &operation.CreatedAt, &operation.UpdatedAt,
	)
	if err != nil {
		return err
	}
	*operation = MergeOperation{ID: operation.ID, MergeRequestID: operation.MergeRequestID,
		RepositoryID: operation.RepositoryID, SourceRevision: operation.SourceRevision,
		TargetRevision: operation.TargetRevision, State: operation.State, Version: operation.Version,
		LeaseExpiresAt: operation.LeaseExpiresAt, StartedAt: operation.StartedAt,
		CompletedAt: operation.CompletedAt, CreatedAt: operation.CreatedAt, UpdatedAt: operation.UpdatedAt}
	if actorID != nil {
		operation.ActorID = *actorID
	}
	if staged != nil {
		operation.StagedRevision = *staged
	}
	if pushed != nil {
		operation.PushedRevision = *pushed
	}
	if errorCode != nil {
		operation.ErrorCode = *errorCode
	}
	if errorDetail != nil {
		operation.ErrorDetail = *errorDetail
	}
	if leaseOwner != nil {
		operation.LeaseOwner = *leaseOwner
	}
	if len(conflictJSON) > 0 && json.Unmarshal(conflictJSON, &operation.ConflictPaths) != nil {
		return errors.New("merge operation conflict paths are invalid")
	}
	if operation.ConflictPaths == nil {
		operation.ConflictPaths = []string{}
	}
	if len(parentJSON) > 0 && json.Unmarshal(parentJSON, &operation.ParentRevisions) != nil {
		return errors.New("merge operation parent revisions are invalid")
	}
	if operation.ParentRevisions == nil {
		operation.ParentRevisions = []string{}
	}
	if operation.Resolutions == nil {
		operation.Resolutions = []MergeResolution{}
	}
	return nil
}

func mustJSON(values []string) []byte {
	if values == nil {
		values = []string{}
	}
	encoded, _ := json.Marshal(values)
	return encoded
}

type mergeRowsQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func loadMergeResolutions(
	ctx context.Context,
	queryer mergeRowsQueryer,
	operationID string,
) ([]MergeResolution, error) {
	rows, err := queryer.Query(ctx, `
		SELECT mor.path, mor.strategy, COALESCE(u.username, ''), mor.created_at, mor.updated_at
		FROM merge_operation_resolutions mor
		LEFT JOIN users u ON u.id = mor.actor_id
		WHERE mor.operation_id = $1
		ORDER BY mor.path
	`, operationID)
	if err != nil {
		return nil, fmt.Errorf("list merge resolutions: %w", err)
	}
	defer rows.Close()
	resolutions := make([]MergeResolution, 0)
	for rows.Next() {
		var resolution MergeResolution
		if err := rows.Scan(&resolution.Path, &resolution.Strategy, &resolution.Actor,
			&resolution.CreatedAt, &resolution.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan merge resolution: %w", err)
		}
		resolutions = append(resolutions, resolution)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate merge resolutions: %w", err)
	}
	return resolutions, nil
}

func scanMergeRequestByTxForUpdate(ctx context.Context, tx pgx.Tx, repoID string, number int64) (MergeRequest, error) {
	row := tx.QueryRow(ctx, `
		SELECT mr.id, mr.number, mr.title, mr.body, mr.state, mr.is_draft,
		       mr.source_branch, mr.target_branch, mr.source_revision, mr.target_revision,
		       author.username, mr.author_id, merged.username, mr.merged_revision,
		       mr.merged_at, mr.created_at, mr.updated_at, mr.closed_at
		FROM merge_requests mr
		JOIN users author ON author.id = mr.author_id
		LEFT JOIN users merged ON merged.id = mr.merged_by
		WHERE mr.repository_id = $1 AND mr.number = $2
		FOR UPDATE OF mr
	`, repoID, number)
	mr, err := scanMergeRequest(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return MergeRequest{}, platform.ErrNotFound
	}
	if err != nil {
		return MergeRequest{}, fmt.Errorf("lock merge request: %w", err)
	}
	return mr, nil
}
