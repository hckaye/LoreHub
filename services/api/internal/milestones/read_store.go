package milestones

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type milestoneQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

const milestoneSelect = `
	SELECT milestone.id, milestone.number, milestone.title, milestone.description,
	       milestone.state, to_char(milestone.due_on, 'YYYY-MM-DD'),
	       creator.username, closer.username, milestone.closed_at,
	       COUNT(issue.id) FILTER (WHERE issue.state = 'open'),
	       COUNT(issue.id) FILTER (WHERE issue.state = 'closed'),
	       milestone.version, milestone.created_at, milestone.updated_at
	FROM repository_milestones milestone
	JOIN users creator ON creator.id = milestone.created_by
	LEFT JOIN users closer ON closer.id = milestone.closed_by
	LEFT JOIN issues issue ON issue.milestone_id = milestone.id
`

func (store *store) List(
	ctx context.Context,
	repositoryID string,
	state string,
	page int,
	perPage int,
) (MilestonePage, error) {
	offset := (page - 1) * perPage
	rows, err := store.pool.Query(ctx, milestoneSelect+`
		WHERE milestone.repository_id = $1 AND ($2 = 'all' OR milestone.state = $2)
		GROUP BY milestone.id, creator.username, closer.username
		ORDER BY (milestone.state = 'open') DESC,
		         milestone.due_on ASC NULLS LAST, milestone.number DESC
		LIMIT $3 OFFSET $4
	`, repositoryID, state, perPage+1, offset)
	if err != nil {
		return MilestonePage{}, fmt.Errorf("list milestones: %w", err)
	}
	defer rows.Close()
	items := make([]Milestone, 0, perPage+1)
	for rows.Next() {
		milestone, scanErr := scanMilestone(rows)
		if scanErr != nil {
			return MilestonePage{}, fmt.Errorf("scan milestone: %w", scanErr)
		}
		items = append(items, milestone)
	}
	if err := rows.Err(); err != nil {
		return MilestonePage{}, fmt.Errorf("iterate milestones: %w", err)
	}
	hasNext := len(items) > perPage
	if hasNext {
		items = items[:perPage]
	}
	return MilestonePage{Milestones: items, Page: page, PerPage: perPage, HasNext: hasNext}, nil
}

func (store *store) Get(ctx context.Context, repositoryID string, number int64) (Milestone, error) {
	return loadMilestone(ctx, store.pool, repositoryID, number)
}

func loadMilestone(
	ctx context.Context,
	database milestoneQueryer,
	repositoryID string,
	number int64,
) (Milestone, error) {
	milestone, err := scanMilestone(database.QueryRow(ctx, milestoneSelect+`
		WHERE milestone.repository_id = $1 AND milestone.number = $2
		GROUP BY milestone.id, creator.username, closer.username
	`, repositoryID, number))
	if errors.Is(err, pgx.ErrNoRows) {
		return Milestone{}, platform.ErrNotFound
	}
	if err != nil {
		return Milestone{}, fmt.Errorf("get milestone: %w", err)
	}
	return milestone, nil
}

func scanMilestone(row pgx.Row) (Milestone, error) {
	var milestone Milestone
	err := row.Scan(
		&milestone.ID, &milestone.Number, &milestone.Title, &milestone.Description,
		&milestone.State, &milestone.DueOn, &milestone.CreatedBy,
		&milestone.ClosedBy, &milestone.ClosedAt,
		&milestone.OpenIssueCount, &milestone.ClosedIssueCount,
		&milestone.Version, &milestone.CreatedAt, &milestone.UpdatedAt,
	)
	return milestone, err
}
