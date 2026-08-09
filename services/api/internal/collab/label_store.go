package collab

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

// ListLabels returns a paginated label list ordered by name.
func (s *store) ListLabels(
	ctx context.Context,
	repoID string,
	page Page,
) (Result[Label], error) {
	offset, err := pageOffset(page)
	if err != nil {
		return Result[Label]{}, err
	}
	limit := page.Limit
	if limit < 1 {
		limit = defaultPageLimit
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, repository_id, name, description, color, created_at
		FROM labels
		WHERE repository_id = $1
		ORDER BY name ASC
		LIMIT $2 OFFSET $3
	`, repoID, limit+1, offset)
	if err != nil {
		return Result[Label]{}, fmt.Errorf("list labels: %w", err)
	}
	defer rows.Close()
	labels := make([]Label, 0)
	for rows.Next() {
		var label Label
		if err := rows.Scan(
			&label.ID, &label.RepositoryID, &label.Name, &label.Description,
			&label.Color, &label.CreatedAt,
		); err != nil {
			return Result[Label]{}, fmt.Errorf("scan label: %w", err)
		}
		labels = append(labels, label)
	}
	if err := rows.Err(); err != nil {
		return Result[Label]{}, fmt.Errorf("iterate labels: %w", err)
	}
	return paginate(labels, limit, offset), nil
}

// CreateLabel inserts a new label. Requires write+ permission. Duplicate names
// return ErrConflict.
func (s *store) CreateLabel(
	ctx context.Context,
	actor platform.User,
	repoID string,
	input LabelInput,
) (Label, error) {
	orgID, err := s.repoOrgID(ctx, repoID)
	if err != nil {
		return Label{}, err
	}
	access, err := s.permFromRef(ctx, actor, repoID, orgID)
	if err != nil {
		return Label{}, err
	}
	if !access.AtLeast(PermWrite) {
		return Label{}, platform.ErrForbidden
	}
	label := Label{
		ID:           uuidArg(),
		RepositoryID: repoID,
		Name:         input.Name,
		Description:  input.Description,
		Color:        input.Color,
		CreatedAt:    nowUTC(),
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Label{}, fmt.Errorf("begin label transaction: %w", err)
	}
	defer rollback(ctx, tx)
	_, err = tx.Exec(ctx, `
		INSERT INTO labels (id, repository_id, name, description, color, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, label.ID, repoID, label.Name, label.Description, label.Color, label.CreatedAt)
	if err != nil {
		return Label{}, translateConstraintError("create label", err)
	}
	if err := insertAudit(ctx, tx, actor.ID, orgID, repoID, "label.create", "label", label.ID); err != nil {
		return Label{}, err
	}
	if err := insertOutbox(ctx, tx, "label.created", label.ID, label); err != nil {
		return Label{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Label{}, fmt.Errorf("commit label transaction: %w", err)
	}
	return label, nil
}

// UpdateLabel mutates an existing label definition. Requires write+ permission
// on the label's repository.
func (s *store) UpdateLabel(
	ctx context.Context,
	actor platform.User,
	repoID string,
	labelID string,
	input LabelInput,
) (Label, error) {
	existing, err := s.findLabel(ctx, repoID, labelID)
	if err != nil {
		return Label{}, err
	}
	access, err := s.permFromRef(ctx, actor, existing.RepositoryID, existing.OrgID)
	if err != nil {
		return Label{}, err
	}
	if !access.AtLeast(PermWrite) {
		return Label{}, platform.ErrForbidden
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Label{}, fmt.Errorf("begin label update: %w", err)
	}
	defer rollback(ctx, tx)
	tag, err := tx.Exec(ctx, `
		UPDATE labels
		SET name = $3, description = $4, color = $5
		WHERE id = $1 AND repository_id = $2
	`, labelID, repoID, input.Name, input.Description, input.Color)
	if err != nil {
		return Label{}, translateConstraintError("update label", err)
	}
	if tag.RowsAffected() == 0 {
		return Label{}, platform.ErrNotFound
	}
	if err := insertAudit(ctx, tx, actor.ID, existing.OrgID,
		existing.RepositoryID, "label.update", "label", labelID); err != nil {
		return Label{}, err
	}
	updated := existing.Label
	updated.Name = input.Name
	updated.Description = input.Description
	updated.Color = input.Color
	if err := insertOutbox(ctx, tx, "label.updated", labelID+":"+uuidArg(), updated); err != nil {
		return Label{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Label{}, fmt.Errorf("commit label update: %w", err)
	}
	return updated, nil
}

// DeleteLabel removes a label definition and its issue associations (via
// cascade). Requires write+ permission on the label's repository.
func (s *store) DeleteLabel(
	ctx context.Context,
	actor platform.User,
	repoID string,
	labelID string,
) error {
	existing, err := s.findLabel(ctx, repoID, labelID)
	if err != nil {
		return err
	}
	access, err := s.permFromRef(ctx, actor, existing.RepositoryID, existing.OrgID)
	if err != nil {
		return err
	}
	if !access.AtLeast(PermWrite) {
		return platform.ErrForbidden
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin label delete: %w", err)
	}
	defer rollback(ctx, tx)
	tag, err := tx.Exec(ctx, `
		DELETE FROM labels WHERE id = $1 AND repository_id = $2
	`, labelID, repoID)
	if err != nil {
		return translateConstraintError("delete label", err)
	}
	if tag.RowsAffected() == 0 {
		return platform.ErrNotFound
	}
	if err := insertAudit(ctx, tx, actor.ID, existing.OrgID,
		existing.RepositoryID, "label.delete", "label", labelID); err != nil {
		return err
	}
	if err := insertOutbox(ctx, tx, "label.deleted", labelID+":"+uuidArg(), existing.Label); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit label delete: %w", err)
	}
	return nil
}

// ApplyLabel attaches a label to an issue. Requires triage+ permission. The
// operation is idempotent: a duplicate apply returns the existing label with
// applied=false. A label or issue that does not belong to the repository
// returns ErrNotFound.
func (s *store) ApplyLabel(
	ctx context.Context,
	actor platform.User,
	repoID string,
	issueNumber int64,
	labelID string,
) (Label, bool, error) {
	label, err := s.findLabel(ctx, repoID, labelID)
	if err != nil {
		return Label{}, false, err
	}
	access, err := s.permFromRef(ctx, actor, repoID, label.OrgID)
	if err != nil {
		return Label{}, false, err
	}
	if !access.AtLeast(PermTriage) {
		return Label{}, false, platform.ErrForbidden
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Label{}, false, fmt.Errorf("begin apply label: %w", err)
	}
	defer rollback(ctx, tx)

	var issueID string
	err = tx.QueryRow(ctx, `
		SELECT id FROM issues WHERE repository_id = $1 AND number = $2
	`, repoID, issueNumber).Scan(&issueID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Label{}, false, platform.ErrNotFound
	}
	if err != nil {
		return Label{}, false, fmt.Errorf("find issue for label: %w", err)
	}

	tag, err := tx.Exec(ctx, `
		INSERT INTO issue_labels (issue_id, label_id) VALUES ($1, $2)
		ON CONFLICT (issue_id, label_id) DO NOTHING
	`, issueID, labelID)
	if err != nil {
		return Label{}, false, translateConstraintError("apply label", err)
	}
	applied := tag.RowsAffected() > 0
	if applied {
		if err := insertAudit(ctx, tx, actor.ID, label.OrgID, repoID,
			"issue_label.apply", "issue", issueID); err != nil {
			return Label{}, false, err
		}
		if err := insertOutbox(ctx, tx, "issue_label.applied", issueID+":"+labelID+":"+uuidArg(), map[string]any{
			"issueId": issueID, "labelId": labelID,
		}); err != nil {
			return Label{}, false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Label{}, false, fmt.Errorf("commit apply label: %w", err)
	}
	return label.Label, applied, nil
}

// RemoveLabel detaches a label from an issue. Requires triage+ permission.
// The operation is idempotent for absent associations; missing issue or label
// returns ErrNotFound.
func (s *store) RemoveLabel(
	ctx context.Context,
	actor platform.User,
	repoID string,
	issueNumber int64,
	labelID string,
) error {
	label, err := s.findLabel(ctx, repoID, labelID)
	if err != nil {
		return err
	}
	access, err := s.permFromRef(ctx, actor, repoID, label.OrgID)
	if err != nil {
		return err
	}
	if !access.AtLeast(PermTriage) {
		return platform.ErrForbidden
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin remove label: %w", err)
	}
	defer rollback(ctx, tx)

	var issueID string
	err = tx.QueryRow(ctx, `
		SELECT id FROM issues WHERE repository_id = $1 AND number = $2
	`, repoID, issueNumber).Scan(&issueID)
	if errors.Is(err, pgx.ErrNoRows) {
		return platform.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("find issue for label removal: %w", err)
	}
	tag, err := tx.Exec(ctx, `
		DELETE FROM issue_labels WHERE issue_id = $1 AND label_id = $2
	`, issueID, labelID)
	if err != nil {
		return translateConstraintError("remove label", err)
	}
	if tag.RowsAffected() > 0 {
		if err := insertAudit(ctx, tx, actor.ID, label.OrgID, repoID,
			"issue_label.remove", "issue", issueID); err != nil {
			return err
		}
		if err := insertOutbox(ctx, tx, "issue_label.removed", issueID+":"+labelID+":"+uuidArg(), map[string]any{
			"issueId": issueID, "labelId": labelID,
		}); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit remove label: %w", err)
	}
	return nil
}

type labelRef struct {
	Label
	OrgID string
}

func (s *store) findLabel(ctx context.Context, repoID, labelID string) (labelRef, error) {
	var ref labelRef
	err := s.pool.QueryRow(ctx, `
		SELECT l.id, l.repository_id, l.name, l.description, l.color, l.created_at,
		       r.organization_id
		FROM labels l
		JOIN repositories r ON r.id = l.repository_id
		WHERE l.repository_id = $1 AND l.id = $2
	`, repoID, labelID).Scan(
		&ref.ID, &ref.RepositoryID, &ref.Name, &ref.Description, &ref.Color,
		&ref.CreatedAt, &ref.OrgID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return labelRef{}, platform.ErrNotFound
	}
	if err != nil {
		return labelRef{}, fmt.Errorf("find label: %w", err)
	}
	return ref, nil
}

func (s *store) repoOrgID(ctx context.Context, repoID string) (string, error) {
	var orgID string
	err := s.pool.QueryRow(ctx, `
		SELECT organization_id FROM repositories WHERE id = $1
	`, repoID).Scan(&orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", platform.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("find repository organization: %w", err)
	}
	return orgID, nil
}
