package collab

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
)

// AuthorizeLoreMergePush is the control-plane boundary immediately before a
// Lore BranchPush. The row lock makes the authorization marker an atomic,
// idempotent consume point for workers and HTTP requests alike.
func (s *store) AuthorizeLoreMergePush(
	ctx context.Context,
	input loreclient.PushAuthorization,
) error {
	if err := validatePushAuthorization(input); err != nil {
		return err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin Lore push authorization: %w", err)
	}
	defer rollback(ctx, tx)

	var (
		operationID, mergeRequestID, repositoryID, actorID    string
		partition, targetBranch                               string
		sourceRevision, targetRevision, stagedRevision        string
		state, leaseOwner                                     string
		leaseExpires, authorizedAt                            *time.Time
		authorizedRevision, authorizedActor, authorizedBranch *string
	)
	err = tx.QueryRow(ctx, `
		SELECT mo.id, mo.merge_request_id, mo.repository_id, COALESCE(mo.actor_id::text, ''),
		       r.lore_repository_id, mr.target_branch, mo.source_revision, mo.target_revision,
		       COALESCE(mo.staged_revision, ''), mo.state, COALESCE(mo.lease_owner, ''),
		       mo.lease_expires_at,
		       mo.push_authorized_revision, mo.push_authorized_actor_id::text,
		       mo.push_authorized_target_branch_id, mo.push_authorized_at
		FROM merge_operations mo
		JOIN merge_requests mr ON mr.id = mo.merge_request_id
		JOIN repositories r ON r.id = mo.repository_id
		WHERE mo.id = $1 AND mo.repository_id = $2 AND mr.target_branch = $3
		FOR UPDATE
	`, input.OperationID, input.RepositoryID, input.TargetBranchName).Scan(
		&operationID, &mergeRequestID, &repositoryID, &actorID, &partition, &targetBranch,
		&sourceRevision, &targetRevision, &stagedRevision, &state, &leaseOwner, &leaseExpires,
		&authorizedRevision, &authorizedActor, &authorizedBranch, &authorizedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return loreclient.ErrPushAuthorizationDenied
	}
	if err != nil {
		return fmt.Errorf("load Lore push authorization: %w", err)
	}
	if operationID != input.OperationID || mergeRequestID == "" || repositoryID != input.RepositoryID ||
		actorID != input.ActorUserID || partition != input.RepositoryPartition || targetBranch != input.TargetBranchName ||
		sourceRevision != input.SourceRevision || targetRevision != input.ExpectedTargetRevision ||
		state != "pushing" || leaseOwner == "" ||
		leaseExpires == nil || !leaseExpires.After(nowUTC()) {
		return loreclient.ErrPushAuthorizationDenied
	}
	if authorizedRevision != nil {
		if *authorizedRevision == input.ProposedRevision && authorizedActor != nil &&
			*authorizedActor == input.ActorUserID && authorizedBranch != nil &&
			*authorizedBranch == input.TargetBranchID {
			if err := tx.Commit(ctx); err != nil {
				return fmt.Errorf("commit repeated Lore push authorization: %w", err)
			}
			return nil
		}
		return loreclient.ErrPushAuthorizationDenied
	}
	if stagedRevision != input.ProposedRevision {
		var hasResolutions bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM merge_operation_resolutions WHERE operation_id = $1
			)
		`, input.OperationID).Scan(&hasResolutions); err != nil {
			return fmt.Errorf("check Lore merge resolution replay: %w", err)
		}
		if !hasResolutions {
			return loreclient.ErrPushAuthorizationDenied
		}
	}
	if err := validateObservedBranch(ctx, tx, input); err != nil {
		return err
	}
	if err := validatePushPermission(ctx, tx, input); err != nil {
		return err
	}
	if err := validateCurrentMergePolicy(ctx, tx, mergeRequestID, input); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE merge_operations
		SET staged_revision = $2, parent_revisions = $3,
		    push_authorized_revision = $2, push_authorized_actor_id = $4,
		    push_authorized_target_branch_id = $5, push_authorized_at = now(),
		    updated_at = now()
		WHERE id = $1 AND state = 'pushing' AND push_authorized_revision IS NULL
	`, input.OperationID, input.ProposedRevision, mustJSON(input.ParentRevisions), input.ActorUserID,
		input.TargetBranchID); err != nil {
		return fmt.Errorf("record Lore push authorization: %w", err)
	}
	var organizationID string
	if err := tx.QueryRow(ctx, `SELECT organization_id FROM repositories WHERE id = $1`, input.RepositoryID).
		Scan(&organizationID); err != nil {
		return fmt.Errorf("find Lore push authorization organization: %w", err)
	}
	if err := insertAudit(ctx, tx, input.ActorUserID, organizationID, input.RepositoryID,
		"merge_operation.push_authorized", "merge_operation", input.OperationID); err != nil {
		return err
	}
	if err := insertOutbox(ctx, tx, "merge_operation.push_authorized",
		input.OperationID+":"+input.ProposedRevision, map[string]string{
			"operationId": input.OperationID, "repositoryId": input.RepositoryID,
			"actorUserId": input.ActorUserID, "proposedRevision": input.ProposedRevision,
		}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit Lore push authorization: %w", err)
	}
	return nil
}

func validatePushAuthorization(input loreclient.PushAuthorization) error {
	if input.ActorUserID == "" || input.RepositoryID == "" || input.RepositoryPartition == "" ||
		input.OperationID == "" || input.TargetBranchID == "" || input.TargetBranchName == "" ||
		input.ExpectedTargetRevision == "" || input.ProposedRevision == "" || input.SourceRevision == "" ||
		!exactParentValues(input.ParentRevisions, input.SourceRevision, input.ExpectedTargetRevision) {
		return loreclient.ErrPushAuthorizationDenied
	}
	return nil
}

func validateObservedBranch(ctx context.Context, tx pgx.Tx, input loreclient.PushAuthorization) error {
	var branchID string
	err := tx.QueryRow(ctx, `
		SELECT branch_id
		FROM repository_branch_states
		WHERE repository_id = $1 AND branch_name = $2 AND latest_revision = $3
		ORDER BY observed_at DESC
		LIMIT 1
	`, input.RepositoryID, input.TargetBranchName, input.ExpectedTargetRevision).Scan(&branchID)
	if errors.Is(err, pgx.ErrNoRows) {
		return loreclient.ErrPushAuthorizationDenied
	}
	if err != nil {
		return fmt.Errorf("check observed Lore target branch: %w", err)
	}
	if branchID != input.TargetBranchID {
		return loreclient.ErrPushAuthorizationDenied
	}
	return nil
}

func validatePushPermission(ctx context.Context, tx pgx.Tx, input loreclient.PushAuthorization) error {
	var allowed bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM repositories r
			JOIN organizations o ON o.id = r.organization_id
			LEFT JOIN repository_memberships rm
				ON rm.repository_id = r.id AND rm.user_id = $2
			LEFT JOIN organization_memberships om
				ON om.organization_id = o.id AND om.user_id = $2
			WHERE r.id = $1 AND (rm.role IN ('write', 'admin') OR om.role IN ('maintainer', 'owner'))
		)
	`, input.RepositoryID, input.ActorUserID).Scan(&allowed)
	if err != nil {
		return fmt.Errorf("check Lore push permission: %w", err)
	}
	if !allowed {
		return loreclient.ErrPushAuthorizationDenied
	}
	return nil
}

func validateCurrentMergePolicy(
	ctx context.Context,
	tx pgx.Tx,
	mergeRequestID string,
	input loreclient.PushAuthorization,
) error {
	rules, err := branchRulesForTransaction(ctx, tx, input.RepositoryID)
	if err != nil {
		return err
	}
	matched := MatchingBranchRules(rules, input.TargetBranchName)
	var approvals, changes int64
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE decision = 'approved'),
		       COUNT(*) FILTER (WHERE decision = 'changes_requested')
		FROM merge_request_reviews
		WHERE merge_request_id = $1 AND source_revision = $2
	`, mergeRequestID, input.SourceRevision).Scan(&approvals, &changes); err != nil {
		return fmt.Errorf("check current Lore merge reviews: %w", err)
	}
	if changes > 0 || approvals < int64(RequiredBranchApprovals(matched)) {
		return loreclient.ErrPushAuthorizationDenied
	}
	if BranchRequiresCI(matched) {
		var success bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM ci_runs
				JOIN merge_requests mr ON mr.repository_id = ci_runs.repository_id
				WHERE ci_runs.repository_id = $1 AND ci_runs.branch = mr.source_branch
				  AND ci_runs.revision = $2 AND ci_runs.status = 'completed'
				  AND ci_runs.conclusion = 'success' AND mr.id = $3
			)
		`, input.RepositoryID, input.SourceRevision, mergeRequestID).Scan(&success); err != nil {
			return fmt.Errorf("check exact Lore merge CI: %w", err)
		}
		if !success {
			return loreclient.ErrPushAuthorizationDenied
		}
	}
	return nil
}

func branchRulesForTransaction(ctx context.Context, tx pgx.Tx, repositoryID string) ([]BranchRule, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, repository_id, pattern, required_approvals,
		       require_ci_success, block_direct_push, created_at, updated_at
		FROM branch_rules WHERE repository_id = $1 ORDER BY pattern ASC
	`, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list Lore merge branch rules: %w", err)
	}
	defer rows.Close()
	rules := make([]BranchRule, 0)
	for rows.Next() {
		var rule BranchRule
		if err := rows.Scan(&rule.ID, &rule.RepositoryID, &rule.Pattern, &rule.RequiredApprovals,
			&rule.RequireCISuccess, &rule.BlockDirectPush, &rule.CreatedAt, &rule.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan Lore merge branch rule: %w", err)
		}
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Lore merge branch rules: %w", err)
	}
	return rules, nil
}

func exactParentValues(parents []string, source, target string) bool {
	if len(parents) != 2 {
		return false
	}
	return (parents[0] == source && parents[1] == target) ||
		(parents[0] == target && parents[1] == source)
}
