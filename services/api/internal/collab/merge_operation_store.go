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
	UpdateMergeOperation(ctx context.Context, operation MergeOperation) (MergeOperation, error)
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
	return operation, nil
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
			source_revision, target_revision, staged_revision, pushed_revision,
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
			if errors.Is(lookupErr, pgx.ErrNoRows) {
				return MergeOperation{}, platform.ErrNotFound
			}
			if lookupErr != nil {
				return MergeOperation{}, fmt.Errorf("lock concurrent merge operation: %w", lookupErr)
			}
		} else {
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
	if operation.State != "aborted" &&
		(operation.SourceRevision != sourceRevision || operation.TargetRevision != targetRevision) {
		return MergeOperation{}, ErrMergeOperationConflict
	}
	reset := operation.State == "aborted"
	state := operation.State
	if reset {
		state = "created"
	}
	if reset {
		operation.StagedRevision = ""
		operation.PushedRevision = ""
		operation.ConflictPaths = []string{}
		operation.ErrorCode = ""
		operation.ErrorDetail = ""
		operation.StartedAt = nil
		operation.CompletedAt = nil
		operation.SourceRevision = sourceRevision
		operation.TargetRevision = targetRevision
	}
	updateRow := tx.QueryRow(ctx, `
		UPDATE merge_operations
		SET actor_id = $2, source_revision = $3, target_revision = $4,
		    state = $5, conflict_paths = $6, staged_revision = NULLIF($7, ''),
		    pushed_revision = NULLIF($8, ''), error_code = NULLIF($9, ''),
		    error_detail = NULLIF($10, ''), lease_owner = $11,
		    lease_expires_at = $12, version = version + 1, updated_at = $13
		WHERE id = $1
		RETURNING id, merge_request_id, repository_id, actor_id,
			source_revision, target_revision, staged_revision, pushed_revision,
			state, conflict_paths, error_code, error_detail, lease_owner,
			lease_expires_at, version, started_at, completed_at, created_at, updated_at
	`, operation.ID, actorID, operation.SourceRevision, operation.TargetRevision,
		state, mustJSON(operation.ConflictPaths), operation.StagedRevision, operation.PushedRevision,
		operation.ErrorCode, operation.ErrorDetail, owner, leaseUntil, now)
	if err := scanMergeOperationInto(updateRow, &operation); err != nil {
		return MergeOperation{}, fmt.Errorf("renew merge operation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return MergeOperation{}, fmt.Errorf("commit merge operation renewal: %w", err)
	}
	return operation, nil
}

func (s *store) UpdateMergeOperation(ctx context.Context, operation MergeOperation) (MergeOperation, error) {
	if operation.ID == "" {
		return MergeOperation{}, ErrMergeOperationConflict
	}
	now := nowUTC()
	row := s.pool.QueryRow(ctx, `
		UPDATE merge_operations
		SET source_revision = $2, target_revision = $3, staged_revision = NULLIF($4, ''),
		    pushed_revision = NULLIF($5, ''), state = $6, conflict_paths = $7,
		    error_code = NULLIF($8, ''), error_detail = NULLIF($9, ''),
		    lease_owner = NULLIF($10, ''), lease_expires_at = $11,
		    started_at = $12, completed_at = $13, version = version + 1, updated_at = $14
		WHERE id = $1 AND version = $15
		RETURNING id, merge_request_id, repository_id, actor_id,
			source_revision, target_revision, staged_revision, pushed_revision,
			state, conflict_paths, error_code, error_detail, lease_owner,
			lease_expires_at, version, started_at, completed_at, created_at, updated_at
	`, operation.ID, operation.SourceRevision, operation.TargetRevision, operation.StagedRevision,
		operation.PushedRevision, operation.State, mustJSON(operation.ConflictPaths), operation.ErrorCode,
		operation.ErrorDetail, operation.LeaseOwner, operation.LeaseExpiresAt, operation.StartedAt,
		operation.CompletedAt, now, operation.Version)
	var updated MergeOperation
	err := scanMergeOperationInto(row, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return MergeOperation{}, ErrMergeOperationConflict
	}
	if err != nil {
		return MergeOperation{}, fmt.Errorf("update merge operation: %w", err)
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
		if operation.State != "merged" {
			_, err = tx.Exec(ctx, `
				UPDATE merge_operations
				SET state = 'merged', pushed_revision = COALESCE(pushed_revision, $2),
				    lease_owner = NULL, lease_expires_at = NULL, completed_at = COALESCE(completed_at, now()),
				    version = version + 1, updated_at = now()
				WHERE id = $1
			`, operation.ID, pushedRevision)
			if err != nil {
				return MergeRequest{}, fmt.Errorf("reconcile merge operation: %w", err)
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return MergeRequest{}, fmt.Errorf("commit merge reconciliation: %w", err)
		}
		return mr, nil
	}
	if operation.State != "pushed" || pushedRevision == "" {
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
		WHERE repository_id = $1 AND number = $2 AND state = 'open'
		RETURNING id
	`, repositoryID, number, actor.ID, pushedRevision, now).Scan(&mr.ID); err != nil {
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
	if err := tx.Commit(ctx); err != nil {
		return MergeRequest{}, fmt.Errorf("commit merge finalization: %w", err)
	}
	return mr, nil
}

const mergeOperationQuery = `
	SELECT mo.id, mo.merge_request_id, mo.repository_id, mo.actor_id,
	       mo.source_revision, mo.target_revision, mo.staged_revision, mo.pushed_revision,
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
	var conflictJSON []byte
	err := row.Scan(
		&operation.ID, &operation.MergeRequestID, &operation.RepositoryID, &actorID,
		&operation.SourceRevision, &operation.TargetRevision, &staged, &pushed,
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
	return nil
}

func mustJSON(values []string) []byte {
	if values == nil {
		values = []string{}
	}
	encoded, _ := json.Marshal(values)
	return encoded
}

func scanMergeRequestByTxForUpdate(ctx context.Context, tx pgx.Tx, repoID string, number int64) (MergeRequest, error) {
	row := tx.QueryRow(ctx, `
		SELECT mr.id, mr.number, mr.title, mr.body, mr.state,
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
