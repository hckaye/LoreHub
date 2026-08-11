package projects

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func (s *store) CreateItem(
	ctx context.Context,
	actor platform.User,
	repo RepositoryRef,
	number int64,
	input ItemInput,
) (Project, error) {
	input, err := validateItemInput(input)
	if err != nil {
		return Project{}, err
	}
	tx, err := s.beginWrite(ctx, actor, repo, "project item creation")
	if err != nil {
		return Project{}, err
	}
	defer rollback(ctx, tx)
	projectID, err := lockProject(ctx, tx, repo.ID, number)
	if err != nil {
		return Project{}, err
	}
	if err := requireColumn(ctx, tx, projectID, input.ColumnID); err != nil {
		return Project{}, err
	}
	issueID, mergeRequestID, err := resolveItemContent(ctx, tx, repo.ID, input)
	if err != nil {
		return Project{}, err
	}
	position, err := nextItemPosition(ctx, tx, input.ColumnID)
	if err != nil {
		return Project{}, err
	}
	itemID := uuid.NewString()
	now := nowUTC()
	_, err = tx.Exec(ctx, `
		INSERT INTO project_items (
			id, project_id, repository_id, column_id, kind, issue_id, merge_request_id,
			draft_title, draft_body, position, created_by, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $12)
	`, itemID, projectID, repo.ID, input.ColumnID, input.Kind, issueID, mergeRequestID,
		input.Title, input.Body, position, actor.ID, now)
	if err != nil {
		return Project{}, constraintError("create project item", err)
	}
	return s.finishItemMutation(ctx, tx, actor, repo, number, projectID, itemID, "project.item.create")
}

func resolveItemContent(
	ctx context.Context,
	tx pgx.Tx,
	repoID string,
	input ItemInput,
) (*string, *string, error) {
	var contentID string
	switch input.Kind {
	case "issue":
		err := tx.QueryRow(ctx, `
			SELECT id FROM issues WHERE repository_id = $1 AND number = $2
		`, repoID, *input.IssueNumber).Scan(&contentID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, platform.ErrNotFound
		}
		if err != nil {
			return nil, nil, fmt.Errorf("resolve project issue: %w", err)
		}
		return &contentID, nil, nil
	case "merge_request":
		err := tx.QueryRow(ctx, `
			SELECT id FROM merge_requests WHERE repository_id = $1 AND number = $2
		`, repoID, *input.MergeRequestNumber).Scan(&contentID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, platform.ErrNotFound
		}
		if err != nil {
			return nil, nil, fmt.Errorf("resolve project pull request: %w", err)
		}
		return nil, &contentID, nil
	default:
		return nil, nil, nil
	}
}

func (s *store) UpdateItem(
	ctx context.Context,
	actor platform.User,
	repo RepositoryRef,
	number int64,
	itemID string,
	input ItemUpdate,
) (Project, error) {
	input, err := validateItemUpdate(input)
	if err != nil {
		return Project{}, err
	}
	tx, err := s.beginWrite(ctx, actor, repo, "project item update")
	if err != nil {
		return Project{}, err
	}
	defer rollback(ctx, tx)
	projectID, err := lockProject(ctx, tx, repo.ID, number)
	if err != nil {
		return Project{}, err
	}
	var kind, columnID, title, body string
	var position int64
	err = tx.QueryRow(ctx, `
		SELECT kind, column_id, draft_title, draft_body, position
		FROM project_items WHERE id = $1 AND project_id = $2 FOR UPDATE
	`, itemID, projectID).Scan(&kind, &columnID, &title, &body, &position)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, platform.ErrNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("lock project item: %w", err)
	}
	if kind != "draft" && (input.Title != nil || input.Body != nil) {
		return Project{}, fmt.Errorf("%w: only draft cards can change title or body", ErrInvalidInput)
	}
	if input.Title != nil {
		title = *input.Title
	}
	if input.Body != nil {
		body = *input.Body
	}
	if input.ColumnID != nil && *input.ColumnID != columnID {
		if err := requireColumn(ctx, tx, projectID, *input.ColumnID); err != nil {
			return Project{}, err
		}
		columnID = *input.ColumnID
		position, err = nextItemPosition(ctx, tx, columnID)
		if err != nil {
			return Project{}, err
		}
	}
	_, err = tx.Exec(ctx, `
		UPDATE project_items
		SET column_id = $1, draft_title = $2, draft_body = $3, position = $4, updated_at = $5
		WHERE id = $6 AND project_id = $7
	`, columnID, title, body, position, nowUTC(), itemID, projectID)
	if err != nil {
		return Project{}, constraintError("update project item", err)
	}
	return s.finishItemMutation(ctx, tx, actor, repo, number, projectID, itemID, "project.item.update")
}

func (s *store) DeleteItem(
	ctx context.Context,
	actor platform.User,
	repo RepositoryRef,
	number int64,
	itemID string,
) error {
	tx, err := s.beginWrite(ctx, actor, repo, "project item deletion")
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	projectID, err := lockProject(ctx, tx, repo.ID, number)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM project_items WHERE id = $1 AND project_id = $2`, itemID, projectID)
	if err != nil {
		return fmt.Errorf("delete project item: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return platform.ErrNotFound
	}
	if err := touchProject(ctx, tx, projectID); err != nil {
		return err
	}
	if err := insertAudit(ctx, tx, actor.ID, repo, "project.item.delete", "project_item", itemID); err != nil {
		return err
	}
	if err := insertOutbox(ctx, tx, "project.updated", projectID, map[string]any{
		"projectId": projectID, "deletedItemId": itemID,
	}); err != nil {
		return err
	}
	return commit(ctx, tx, "project item deletion")
}

func requireColumn(ctx context.Context, tx pgx.Tx, projectID string, columnID string) error {
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM project_columns WHERE id = $1 AND project_id = $2)
	`, columnID, projectID).Scan(&exists); err != nil {
		return fmt.Errorf("verify project column: %w", err)
	}
	if !exists {
		return platform.ErrNotFound
	}
	return nil
}

func nextItemPosition(ctx context.Context, tx pgx.Tx, columnID string) (int64, error) {
	var position int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(position), 0) + 1024 FROM project_items WHERE column_id = $1
	`, columnID).Scan(&position); err != nil {
		return 0, fmt.Errorf("allocate project item position: %w", err)
	}
	return position, nil
}

func (s *store) finishItemMutation(
	ctx context.Context,
	tx pgx.Tx,
	actor platform.User,
	repo RepositoryRef,
	number int64,
	projectID string,
	itemID string,
	action string,
) (Project, error) {
	if err := touchProject(ctx, tx, projectID); err != nil {
		return Project{}, err
	}
	project, err := loadProject(ctx, tx, repo.ID, number)
	if err != nil {
		return Project{}, err
	}
	if err := insertAudit(ctx, tx, actor.ID, repo, action, "project_item", itemID); err != nil {
		return Project{}, err
	}
	if err := insertOutbox(ctx, tx, "project.updated", projectID, project); err != nil {
		return Project{}, err
	}
	if err := commit(ctx, tx, "project item mutation"); err != nil {
		return Project{}, err
	}
	project.ViewerCanWrite = true
	return project, nil
}
