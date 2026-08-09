package collab

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

// ListBranchRules returns all branch protection rules for a repository ordered
// by pattern. The set is expected to be small, so pagination is omitted.
func (s *store) ListBranchRules(ctx context.Context, repoID string) ([]BranchRule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, repository_id, pattern, required_approvals,
		       require_ci_success, block_direct_push, created_at, updated_at
		FROM branch_rules
		WHERE repository_id = $1
		ORDER BY pattern ASC
	`, repoID)
	if err != nil {
		return nil, fmt.Errorf("list branch rules: %w", err)
	}
	defer rows.Close()
	rules := make([]BranchRule, 0)
	for rows.Next() {
		var rule BranchRule
		if err := rows.Scan(
			&rule.ID, &rule.RepositoryID, &rule.Pattern, &rule.RequiredApprovals,
			&rule.RequireCISuccess, &rule.BlockDirectPush, &rule.CreatedAt, &rule.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan branch rule: %w", err)
		}
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate branch rules: %w", err)
	}
	return rules, nil
}

// CreateBranchRule inserts a new branch protection rule. Requires admin or
// organization maintainer/owner permission. Duplicate patterns return
// ErrConflict.
func (s *store) CreateBranchRule(
	ctx context.Context,
	actor platform.User,
	repoID string,
	input BranchRuleInput,
) (BranchRule, error) {
	orgID, err := s.repoOrgID(ctx, repoID)
	if err != nil {
		return BranchRule{}, err
	}
	access, err := s.permFromRef(ctx, actor, repoID, orgID)
	if err != nil {
		return BranchRule{}, err
	}
	if !access.CanManageBranchRules() {
		return BranchRule{}, platform.ErrForbidden
	}
	now := nowUTC()
	rule := BranchRule{
		ID:                uuidArg(),
		RepositoryID:      repoID,
		Pattern:           input.Pattern,
		RequiredApprovals: input.RequiredApprovals,
		RequireCISuccess:  input.RequireCISuccess,
		BlockDirectPush:   input.BlockDirectPush,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return BranchRule{}, fmt.Errorf("begin branch rule transaction: %w", err)
	}
	defer rollback(ctx, tx)
	_, err = tx.Exec(ctx, `
		INSERT INTO branch_rules (
			id, repository_id, pattern, required_approvals,
			require_ci_success, block_direct_push, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, rule.ID, repoID, rule.Pattern, rule.RequiredApprovals,
		rule.RequireCISuccess, rule.BlockDirectPush, rule.CreatedAt, rule.UpdatedAt)
	if err != nil {
		return BranchRule{}, translateConstraintError("create branch rule", err)
	}
	if err := insertAudit(ctx, tx, actor.ID, orgID, repoID, "branch_rule.create", "branch_rule", rule.ID); err != nil {
		return BranchRule{}, err
	}
	if err := insertOutbox(ctx, tx, "branch_rule.created", rule.ID, rule); err != nil {
		return BranchRule{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return BranchRule{}, fmt.Errorf("commit branch rule transaction: %w", err)
	}
	return rule, nil
}

// UpdateBranchRule mutates an existing branch protection rule. Requires admin
// or organization maintainer/owner permission.
func (s *store) UpdateBranchRule(
	ctx context.Context,
	actor platform.User,
	repoID string,
	ruleID string,
	input BranchRuleInput,
) (BranchRule, error) {
	existing, err := s.findBranchRule(ctx, repoID, ruleID)
	if err != nil {
		return BranchRule{}, err
	}
	access, err := s.permFromRef(ctx, actor, existing.RepositoryID, existing.OrgID)
	if err != nil {
		return BranchRule{}, err
	}
	if !access.CanManageBranchRules() {
		return BranchRule{}, platform.ErrForbidden
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return BranchRule{}, fmt.Errorf("begin branch rule update: %w", err)
	}
	defer rollback(ctx, tx)
	now := nowUTC()
	tag, err := tx.Exec(ctx, `
		UPDATE branch_rules
		SET pattern = $3, required_approvals = $4,
		    require_ci_success = $5, block_direct_push = $6, updated_at = $7
		WHERE id = $1 AND repository_id = $2
	`, ruleID, repoID, input.Pattern, input.RequiredApprovals,
		input.RequireCISuccess, input.BlockDirectPush, now)
	if err != nil {
		return BranchRule{}, translateConstraintError("update branch rule", err)
	}
	if tag.RowsAffected() == 0 {
		return BranchRule{}, platform.ErrNotFound
	}
	if err := insertAudit(ctx, tx, actor.ID, existing.OrgID,
		existing.RepositoryID, "branch_rule.update", "branch_rule", ruleID); err != nil {
		return BranchRule{}, err
	}
	updated := existing.BranchRule
	updated.Pattern = input.Pattern
	updated.RequiredApprovals = input.RequiredApprovals
	updated.RequireCISuccess = input.RequireCISuccess
	updated.BlockDirectPush = input.BlockDirectPush
	updated.UpdatedAt = now
	if err := insertOutbox(ctx, tx, "branch_rule.updated", ruleID+":"+uuidArg(), updated); err != nil {
		return BranchRule{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return BranchRule{}, fmt.Errorf("commit branch rule update: %w", err)
	}
	return updated, nil
}

// DeleteBranchRule removes a branch protection rule. Requires admin or
// organization maintainer/owner permission.
func (s *store) DeleteBranchRule(
	ctx context.Context,
	actor platform.User,
	repoID string,
	ruleID string,
) error {
	existing, err := s.findBranchRule(ctx, repoID, ruleID)
	if err != nil {
		return err
	}
	access, err := s.permFromRef(ctx, actor, existing.RepositoryID, existing.OrgID)
	if err != nil {
		return err
	}
	if !access.CanManageBranchRules() {
		return platform.ErrForbidden
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin branch rule delete: %w", err)
	}
	defer rollback(ctx, tx)
	tag, err := tx.Exec(ctx, `
		DELETE FROM branch_rules WHERE id = $1 AND repository_id = $2
	`, ruleID, repoID)
	if err != nil {
		return translateConstraintError("delete branch rule", err)
	}
	if tag.RowsAffected() == 0 {
		return platform.ErrNotFound
	}
	if err := insertAudit(ctx, tx, actor.ID, existing.OrgID,
		existing.RepositoryID, "branch_rule.delete", "branch_rule", ruleID); err != nil {
		return err
	}
	if err := insertOutbox(ctx, tx, "branch_rule.deleted", ruleID+":"+uuidArg(), existing.BranchRule); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit branch rule delete: %w", err)
	}
	return nil
}

type branchRuleRef struct {
	BranchRule
	OrgID string
}

func (s *store) findBranchRule(ctx context.Context, repoID, ruleID string) (branchRuleRef, error) {
	var ref branchRuleRef
	err := s.pool.QueryRow(ctx, `
		SELECT br.id, br.repository_id, br.pattern, br.required_approvals,
		       br.require_ci_success, br.block_direct_push, br.created_at, br.updated_at,
		       r.organization_id
		FROM branch_rules br
		JOIN repositories r ON r.id = br.repository_id
		WHERE br.repository_id = $1 AND br.id = $2
	`, repoID, ruleID).Scan(
		&ref.ID, &ref.RepositoryID, &ref.Pattern, &ref.RequiredApprovals,
		&ref.RequireCISuccess, &ref.BlockDirectPush, &ref.CreatedAt, &ref.UpdatedAt,
		&ref.OrgID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return branchRuleRef{}, platform.ErrNotFound
	}
	if err != nil {
		return branchRuleRef{}, fmt.Errorf("find branch rule: %w", err)
	}
	return ref, nil
}
