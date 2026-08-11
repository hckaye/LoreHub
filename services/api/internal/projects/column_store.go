package projects

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func (s *store) CreateColumn(
	ctx context.Context,
	actor platform.User,
	repo RepositoryRef,
	number int64,
	input ColumnInput,
) (Project, error) {
	input, err := validateColumnInput(input)
	if err != nil {
		return Project{}, err
	}
	tx, err := s.beginWrite(ctx, actor, repo, "project column creation")
	if err != nil {
		return Project{}, err
	}
	defer rollback(ctx, tx)
	projectID, err := lockProject(ctx, tx, repo.ID, number)
	if err != nil {
		return Project{}, err
	}
	var position int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(position), 0) + 1024 FROM project_columns WHERE project_id = $1
	`, projectID).Scan(&position); err != nil {
		return Project{}, fmt.Errorf("allocate project column position: %w", err)
	}
	columnID := uuid.NewString()
	now := nowUTC()
	_, err = tx.Exec(ctx, `
		INSERT INTO project_columns (id, project_id, name, position, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $5)
	`, columnID, projectID, input.Name, position, now)
	if err != nil {
		return Project{}, constraintError("create project column", err)
	}
	return s.finishColumnMutation(ctx, tx, actor, repo, number, projectID, columnID, "project.column.create")
}

func (s *store) UpdateColumn(
	ctx context.Context,
	actor platform.User,
	repo RepositoryRef,
	number int64,
	columnID string,
	input ColumnInput,
) (Project, error) {
	input, err := validateColumnInput(input)
	if err != nil {
		return Project{}, err
	}
	tx, err := s.beginWrite(ctx, actor, repo, "project column update")
	if err != nil {
		return Project{}, err
	}
	defer rollback(ctx, tx)
	projectID, err := lockProject(ctx, tx, repo.ID, number)
	if err != nil {
		return Project{}, err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE project_columns SET name = $1, updated_at = $2
		WHERE id = $3 AND project_id = $4
	`, input.Name, nowUTC(), columnID, projectID)
	if err != nil {
		return Project{}, constraintError("update project column", err)
	}
	if tag.RowsAffected() == 0 {
		return Project{}, platform.ErrNotFound
	}
	return s.finishColumnMutation(ctx, tx, actor, repo, number, projectID, columnID, "project.column.update")
}

func (s *store) DeleteColumn(
	ctx context.Context,
	actor platform.User,
	repo RepositoryRef,
	number int64,
	columnID string,
) error {
	tx, err := s.beginWrite(ctx, actor, repo, "project column deletion")
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	projectID, err := lockProject(ctx, tx, repo.ID, number)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM project_columns WHERE id = $1 AND project_id = $2`,
		columnID, projectID)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23503" {
			return ErrColumnNotEmpty
		}
		return fmt.Errorf("delete project column: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return platform.ErrNotFound
	}
	if err := touchProject(ctx, tx, projectID); err != nil {
		return err
	}
	if err := insertAudit(ctx, tx, actor.ID, repo, "project.column.delete", "project_column", columnID); err != nil {
		return err
	}
	if err := insertOutbox(ctx, tx, "project.updated", projectID, map[string]any{
		"projectId": projectID, "deletedColumnId": columnID,
	}); err != nil {
		return err
	}
	return commit(ctx, tx, "project column deletion")
}

func (s *store) finishColumnMutation(
	ctx context.Context,
	tx pgx.Tx,
	actor platform.User,
	repo RepositoryRef,
	number int64,
	projectID string,
	columnID string,
	action string,
) (Project, error) {
	if err := touchProject(ctx, tx, projectID); err != nil {
		return Project{}, err
	}
	project, err := loadProject(ctx, tx, repo.ID, number)
	if err != nil {
		return Project{}, err
	}
	if err := insertAudit(ctx, tx, actor.ID, repo, action, "project_column", columnID); err != nil {
		return Project{}, err
	}
	if err := insertOutbox(ctx, tx, "project.updated", projectID, project); err != nil {
		return Project{}, err
	}
	if err := commit(ctx, tx, "project column mutation"); err != nil {
		return Project{}, err
	}
	project.ViewerCanWrite = true
	return project, nil
}
