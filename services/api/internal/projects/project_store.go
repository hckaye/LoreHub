package projects

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

var defaultColumns = []string{"Todo", "In progress", "Done"}

func (s *store) Create(
	ctx context.Context,
	actor platform.User,
	repo RepositoryRef,
	input ProjectInput,
) (Project, error) {
	input, err := validateProjectInput(input)
	if err != nil {
		return Project{}, err
	}
	tx, err := s.beginWrite(ctx, actor, repo, "project creation")
	if err != nil {
		return Project{}, err
	}
	defer rollback(ctx, tx)

	var number int64
	err = tx.QueryRow(ctx, `
		UPDATE repository_counters
		SET next_project_number = next_project_number + 1
		WHERE repository_id = $1
		RETURNING next_project_number - 1
	`, repo.ID).Scan(&number)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, platform.ErrNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("allocate project number: %w", err)
	}
	projectID := uuid.NewString()
	now := nowUTC()
	_, err = tx.Exec(ctx, `
		INSERT INTO projects (
			id, repository_id, number, title, description, state, created_by, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
	`, projectID, repo.ID, number, input.Title, input.Description, input.State, actor.ID, now)
	if err != nil {
		return Project{}, constraintError("create project", err)
	}
	for index, name := range defaultColumns {
		_, err = tx.Exec(ctx, `
			INSERT INTO project_columns (id, project_id, name, position, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $5)
		`, uuid.NewString(), projectID, name, int64(index+1)*1024, now)
		if err != nil {
			return Project{}, constraintError("create default project column", err)
		}
	}
	project, err := loadProject(ctx, tx, repo.ID, number)
	if err != nil {
		return Project{}, err
	}
	if err := recordProjectMutation(ctx, tx, actor, repo, "project.create", project); err != nil {
		return Project{}, err
	}
	if err := commit(ctx, tx, "project creation"); err != nil {
		return Project{}, err
	}
	project.ViewerCanWrite = true
	return project, nil
}

func (s *store) Update(
	ctx context.Context,
	actor platform.User,
	repo RepositoryRef,
	number int64,
	input ProjectUpdate,
) (Project, error) {
	input, err := validateProjectUpdate(input)
	if err != nil {
		return Project{}, err
	}
	tx, err := s.beginWrite(ctx, actor, repo, "project update")
	if err != nil {
		return Project{}, err
	}
	defer rollback(ctx, tx)
	projectID, err := lockProject(ctx, tx, repo.ID, number)
	if err != nil {
		return Project{}, err
	}
	_, err = tx.Exec(ctx, `
		UPDATE projects
		SET title = COALESCE($1, title),
		    description = COALESCE($2, description),
		    state = COALESCE($3, state),
		    updated_at = $4
		WHERE id = $5 AND repository_id = $6
	`, input.Title, input.Description, input.State, nowUTC(), projectID, repo.ID)
	if err != nil {
		return Project{}, constraintError("update project", err)
	}
	project, err := loadProject(ctx, tx, repo.ID, number)
	if err != nil {
		return Project{}, err
	}
	if err := recordProjectMutation(ctx, tx, actor, repo, "project.update", project); err != nil {
		return Project{}, err
	}
	if err := commit(ctx, tx, "project update"); err != nil {
		return Project{}, err
	}
	project.ViewerCanWrite = true
	return project, nil
}

func (s *store) Delete(
	ctx context.Context,
	actor platform.User,
	repo RepositoryRef,
	number int64,
) error {
	tx, err := s.beginWrite(ctx, actor, repo, "project deletion")
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	projectID, err := lockProject(ctx, tx, repo.ID, number)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM projects WHERE id = $1 AND repository_id = $2`,
		projectID, repo.ID); err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	if err := insertAudit(ctx, tx, actor.ID, repo, "project.delete", "project", projectID); err != nil {
		return err
	}
	if err := insertOutbox(ctx, tx, "project.deleted", projectID, map[string]any{
		"id": projectID, "repositoryId": repo.ID, "number": number,
	}); err != nil {
		return err
	}
	return commit(ctx, tx, "project deletion")
}

func recordProjectMutation(
	ctx context.Context,
	tx pgx.Tx,
	actor platform.User,
	repo RepositoryRef,
	action string,
	project Project,
) error {
	if err := insertAudit(ctx, tx, actor.ID, repo, action, "project", project.ID); err != nil {
		return err
	}
	return insertOutbox(ctx, tx, "project.updated", project.ID, project)
}

func touchProject(ctx context.Context, tx pgx.Tx, projectID string) error {
	if _, err := tx.Exec(ctx, `UPDATE projects SET updated_at = $1 WHERE id = $2`, nowUTC(), projectID); err != nil {
		return fmt.Errorf("touch project: %w", err)
	}
	return nil
}
