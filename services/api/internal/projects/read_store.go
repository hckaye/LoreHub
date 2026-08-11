package projects

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

const projectSummarySelect = `
	SELECT project.id, project.number, project.title, project.description, project.state,
	       creator.username,
	       (SELECT COUNT(*) FROM project_columns column_record WHERE column_record.project_id = project.id),
	       (SELECT COUNT(*) FROM project_items item WHERE item.project_id = project.id),
	       project.created_at, project.updated_at
	FROM projects project
	JOIN users creator ON creator.id = project.created_by
`

func (s *store) List(ctx context.Context, repoID string) ([]ProjectSummary, error) {
	rows, err := s.pool.Query(ctx, projectSummarySelect+`
		WHERE project.repository_id = $1
		ORDER BY (project.state = 'open') DESC, project.updated_at DESC, project.number DESC
	`, repoID)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()
	projects := make([]ProjectSummary, 0)
	for rows.Next() {
		project, scanErr := scanProjectSummary(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan project summary: %w", scanErr)
		}
		projects = append(projects, project)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	return projects, nil
}

func (s *store) Get(ctx context.Context, repoID string, number int64) (Project, error) {
	return loadProject(ctx, s.pool, repoID, number)
}

func loadProject(ctx context.Context, database queryer, repoID string, number int64) (Project, error) {
	summary, err := scanProjectSummary(database.QueryRow(ctx, projectSummarySelect+`
		WHERE project.repository_id = $1 AND project.number = $2
	`, repoID, number))
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, platform.ErrNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("get project: %w", err)
	}
	project := Project{ProjectSummary: summary, Columns: make([]Column, 0)}
	rows, err := database.Query(ctx, `
		SELECT id, name, position, created_at, updated_at
		FROM project_columns
		WHERE project_id = $1
		ORDER BY position, id
	`, project.ID)
	if err != nil {
		return Project{}, fmt.Errorf("list project columns: %w", err)
	}
	for rows.Next() {
		column := Column{Items: make([]Item, 0)}
		if err := rows.Scan(
			&column.ID, &column.Name, &column.Position, &column.CreatedAt, &column.UpdatedAt,
		); err != nil {
			rows.Close()
			return Project{}, fmt.Errorf("scan project column: %w", err)
		}
		project.Columns = append(project.Columns, column)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Project{}, fmt.Errorf("list project columns: %w", err)
	}
	rows.Close()
	if err := loadItems(ctx, database, project.ID, &project); err != nil {
		return Project{}, err
	}
	return project, nil
}

func loadItems(ctx context.Context, database queryer, projectID string, project *Project) error {
	rows, err := database.Query(ctx, `
		SELECT item.id, item.column_id, item.kind,
		       CASE
		           WHEN item.kind = 'issue' THEN issue.number
		           WHEN item.kind = 'merge_request' THEN merge_request.number
		       END,
		       CASE
		           WHEN item.kind = 'issue' THEN issue.title
		           WHEN item.kind = 'merge_request' THEN merge_request.title
		           ELSE item.draft_title
		       END,
		       CASE WHEN item.kind = 'draft' THEN item.draft_body ELSE '' END,
		       CASE
		           WHEN item.kind = 'issue' THEN issue.state
		           WHEN item.kind = 'merge_request' THEN merge_request.state
		           ELSE 'draft'
		       END,
		       CASE
		           WHEN item.kind = 'issue' THEN issue_author.username
		           WHEN item.kind = 'merge_request' THEN merge_request_author.username
		           ELSE creator.username
		       END,
		       item.position, item.created_at,
		       GREATEST(
		           item.updated_at,
		           COALESCE(issue.updated_at, item.updated_at),
		           COALESCE(merge_request.updated_at, item.updated_at)
		       )
		FROM project_items item
		JOIN users creator ON creator.id = item.created_by
		LEFT JOIN issues issue ON issue.id = item.issue_id AND issue.repository_id = item.repository_id
		LEFT JOIN users issue_author ON issue_author.id = issue.author_id
		LEFT JOIN merge_requests merge_request
		  ON merge_request.id = item.merge_request_id
		 AND merge_request.repository_id = item.repository_id
		LEFT JOIN users merge_request_author ON merge_request_author.id = merge_request.author_id
		WHERE item.project_id = $1
		ORDER BY item.column_id, item.position, item.id
	`, projectID)
	if err != nil {
		return fmt.Errorf("list project items: %w", err)
	}
	defer rows.Close()
	columns := make(map[string]*Column, len(project.Columns))
	for index := range project.Columns {
		columns[project.Columns[index].ID] = &project.Columns[index]
	}
	for rows.Next() {
		var item Item
		if err := rows.Scan(
			&item.ID, &item.ColumnID, &item.Kind, &item.Number, &item.Title, &item.Body,
			&item.State, &item.Author, &item.Position, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return fmt.Errorf("scan project item: %w", err)
		}
		column := columns[item.ColumnID]
		if column == nil {
			return errors.New("project item references an unloaded column")
		}
		column.Items = append(column.Items, item)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("list project items: %w", err)
	}
	return nil
}

func scanProjectSummary(row pgx.Row) (ProjectSummary, error) {
	var project ProjectSummary
	err := row.Scan(
		&project.ID, &project.Number, &project.Title, &project.Description, &project.State,
		&project.CreatedBy, &project.ColumnCount, &project.ItemCount,
		&project.CreatedAt, &project.UpdatedAt,
	)
	return project, err
}
