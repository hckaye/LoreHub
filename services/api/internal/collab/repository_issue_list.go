package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

const repositoryIssueListFrom = `
	FROM issues i
	JOIN repositories repository
	  ON repository.id = i.repository_id AND repository.lifecycle_state = 'active'
	JOIN organizations organization
	  ON organization.id = repository.organization_id AND organization.active
	JOIN users author ON author.id = i.author_id
	LEFT JOIN repository_milestones milestone ON milestone.id = i.milestone_id
	LEFT JOIN users closed_by ON closed_by.id = i.closed_by
	WHERE i.repository_id = $1
`

func (s *store) ListIssuesForRepository(
	ctx context.Context,
	repositoryID string,
	query RepositoryIssueQuery,
) (RepositoryIssuePage, error) {
	query, err := NormalizeRepositoryIssueQuery(query)
	if err != nil {
		return RepositoryIssuePage{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return RepositoryIssuePage{}, fmt.Errorf("begin repository issue list: %w", err)
	}
	defer rollback(ctx, tx)
	openCount, closedCount, err := countRepositoryIssueStates(ctx, tx, repositoryID, query)
	if err != nil {
		return RepositoryIssuePage{}, err
	}
	totalCount := openCount + closedCount
	if query.State == "open" {
		totalCount = openCount
	} else if query.State == "closed" {
		totalCount = closedCount
	}
	issues, err := listRepositoryIssues(ctx, tx, repositoryID, query)
	if err != nil {
		return RepositoryIssuePage{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RepositoryIssuePage{}, fmt.Errorf("commit repository issue list: %w", err)
	}
	return RepositoryIssuePage{
		Issues: issues, TotalCount: totalCount, OpenCount: openCount,
		ClosedCount: closedCount, Page: query.Page, PerPage: query.PerPage,
		HasNext: int64(query.Page*query.PerPage) < totalCount,
	}, nil
}

func countRepositoryIssueStates(
	ctx context.Context,
	tx pgx.Tx,
	repositoryID string,
	query RepositoryIssueQuery,
) (int64, int64, error) {
	builder := buildRepositoryIssueFilter(repositoryID, query, false)
	var openCount, closedCount int64
	err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE i.state = 'open'),
		       COUNT(*) FILTER (WHERE i.state = 'closed')
`+repositoryIssueListFrom+builder.where(), builder.arguments...).Scan(&openCount, &closedCount)
	if err != nil {
		return 0, 0, fmt.Errorf("count repository issue states: %w", err)
	}
	return openCount, closedCount, nil
}

func listRepositoryIssues(
	ctx context.Context,
	tx pgx.Tx,
	repositoryID string,
	query RepositoryIssueQuery,
) ([]Issue, error) {
	builder := buildRepositoryIssueFilter(repositoryID, query, true)
	limit := builder.bind(query.PerPage)
	offset := builder.bind((query.Page - 1) * query.PerPage)
	rows, err := tx.Query(ctx, `
		SELECT i.id, i.number, i.title, i.body, i.state, author.username, i.author_id,
		       COALESCE((
		           SELECT jsonb_agg(jsonb_build_object(
		               'id', assigned_user.id,
		               'username', assigned_user.username,
		               'displayName', assigned_user.display_name,
		               'avatarUrl', assigned_user.avatar_url
		           ) ORDER BY assignment.assigned_at, assigned_user.username)
		           FROM issue_assignees assignment
		           JOIN users assigned_user ON assigned_user.id = assignment.user_id
		           WHERE assignment.issue_id = i.id
		       ), '[]'::jsonb),
		       milestone.id, milestone.number, milestone.title,
		       milestone.state, to_char(milestone.due_on, 'YYYY-MM-DD'),
		       COALESCE((
		           SELECT jsonb_agg(jsonb_build_object(
		               'id', label.id,
		               'repositoryId', label.repository_id,
		               'name', label.name,
		               'description', label.description,
		               'color', label.color,
		               'createdAt', label.created_at
		           ) ORDER BY lower(label.name), label.id)
		           FROM issue_labels assignment
		           JOIN labels label ON label.id = assignment.label_id
		           WHERE assignment.issue_id = i.id
		       ), '[]'::jsonb),
		       i.comment_count AS comment_count,
		       i.created_at, i.updated_at, closed_by.username, i.closed_at
`+repositoryIssueListFrom+builder.where()+`
		ORDER BY `+repositoryWorkItemOrder("i", query.Sort, query.Direction)+`
		LIMIT `+limit+` OFFSET `+offset,
		builder.arguments...,
	)
	if err != nil {
		return nil, fmt.Errorf("list repository issues: %w", err)
	}
	defer rows.Close()
	issues := make([]Issue, 0)
	for rows.Next() {
		issue, err := scanRepositoryIssue(rows)
		if err != nil {
			return nil, err
		}
		issues = append(issues, issue)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate repository issues: %w", err)
	}
	return issues, nil
}

func scanRepositoryIssue(row pgx.Row) (Issue, error) {
	var issue Issue
	var assignees, labels json.RawMessage
	var milestoneID, milestoneTitle, milestoneState, milestoneDueOn *string
	var milestoneNumber *int64
	err := row.Scan(
		&issue.ID, &issue.Number, &issue.Title, &issue.Body, &issue.State,
		&issue.Author, &issue.AuthorID, &assignees,
		&milestoneID, &milestoneNumber, &milestoneTitle, &milestoneState, &milestoneDueOn,
		&labels, &issue.CommentCount, &issue.CreatedAt, &issue.UpdatedAt,
		&issue.ClosedBy, &issue.ClosedAt,
	)
	if err != nil {
		return Issue{}, fmt.Errorf("scan repository issue: %w", err)
	}
	if err := json.Unmarshal(assignees, &issue.Assignees); err != nil {
		return Issue{}, fmt.Errorf("decode repository issue assignees: %w", err)
	}
	if err := json.Unmarshal(labels, &issue.Labels); err != nil {
		return Issue{}, fmt.Errorf("decode repository issue labels: %w", err)
	}
	setPrimaryAssignee(&issue)
	issue.LabelCount = int64(len(issue.Labels))
	if milestoneID != nil && milestoneNumber != nil && milestoneTitle != nil && milestoneState != nil {
		issue.Milestone = &MilestoneSummary{
			ID: *milestoneID, Number: *milestoneNumber, Title: *milestoneTitle,
			State: *milestoneState, DueOn: milestoneDueOn,
		}
	}
	return issue, nil
}

func buildRepositoryIssueFilter(
	repositoryID string,
	query RepositoryIssueQuery,
	includeState bool,
) *repositoryQueryBuilder {
	builder := newRepositoryQueryBuilder(repositoryID)
	if includeState && query.State != "all" {
		builder.add("i.state = " + builder.bind(query.State))
	}
	if query.Search != "" {
		pattern := builder.bind(repositorySearchPattern(query.Search))
		builder.add(`(i.title || ' ' || i.body) ILIKE ` + pattern + ` ESCAPE '\'`)
	}
	if query.Author != "" {
		builder.add("lower(author.username) = lower(" + builder.bind(query.Author) + ")")
	}
	addRepositoryIssueAssigneeFilter(builder, query.Assignee)
	for _, label := range query.Labels {
		parameter := builder.bind(label)
		builder.add(`EXISTS (
			SELECT 1 FROM issue_labels filtered_assignment
			JOIN labels filtered_label ON filtered_label.id = filtered_assignment.label_id
			WHERE filtered_assignment.issue_id = i.id
			  AND lower(filtered_label.name) = lower(` + parameter + `)
		)`)
	}
	if query.WithoutMilestone {
		builder.add("i.milestone_id IS NULL")
	} else if query.MilestoneNumber != nil {
		builder.add("milestone.number = " + builder.bind(*query.MilestoneNumber))
	}
	return builder
}

func addRepositoryIssueAssigneeFilter(builder *repositoryQueryBuilder, assignee string) {
	if assignee == "" {
		return
	}
	if strings.EqualFold(assignee, "none") {
		builder.add("NOT EXISTS (SELECT 1 FROM issue_assignees WHERE issue_id = i.id)")
		return
	}
	parameter := builder.bind(assignee)
	builder.add(`EXISTS (
		SELECT 1 FROM issue_assignees filtered_assignment
		JOIN users filtered_user ON filtered_user.id = filtered_assignment.user_id
		WHERE filtered_assignment.issue_id = i.id
		  AND lower(filtered_user.username) = lower(` + parameter + `)
	)`)
}
