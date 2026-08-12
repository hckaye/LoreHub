package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

const repositoryMergeRequestListFrom = `
	FROM merge_requests mr
	JOIN repositories repository
	  ON repository.id = mr.repository_id AND repository.lifecycle_state = 'active'
	JOIN organizations organization
	  ON organization.id = repository.organization_id AND organization.active
	JOIN users author ON author.id = mr.author_id
	LEFT JOIN users merged ON merged.id = mr.merged_by
	LEFT JOIN repository_milestones milestone ON milestone.id = mr.milestone_id
	WHERE mr.repository_id = $1
`

func (s *store) ListMergeRequestsForRepository(
	ctx context.Context,
	repositoryID string,
	query RepositoryMergeRequestQuery,
) (RepositoryMergeRequestPage, error) {
	query, err := NormalizeRepositoryMergeRequestQuery(query)
	if err != nil {
		return RepositoryMergeRequestPage{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return RepositoryMergeRequestPage{}, fmt.Errorf("begin repository pull request list: %w", err)
	}
	defer rollback(ctx, tx)
	openCount, closedCount, mergedCount, err := countRepositoryMergeRequestStates(
		ctx, tx, repositoryID, query,
	)
	if err != nil {
		return RepositoryMergeRequestPage{}, err
	}
	totalCount := openCount + closedCount + mergedCount
	switch query.State {
	case "open":
		totalCount = openCount
	case "closed":
		totalCount = closedCount
	case "merged":
		totalCount = mergedCount
	}
	mergeRequests, err := listRepositoryMergeRequests(ctx, tx, repositoryID, query)
	if err != nil {
		return RepositoryMergeRequestPage{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RepositoryMergeRequestPage{}, fmt.Errorf("commit repository pull request list: %w", err)
	}
	return RepositoryMergeRequestPage{
		MergeRequests: mergeRequests, TotalCount: totalCount,
		OpenCount: openCount, ClosedCount: closedCount, MergedCount: mergedCount,
		Page: query.Page, PerPage: query.PerPage,
		HasNext: int64(query.Page*query.PerPage) < totalCount,
	}, nil
}

func countRepositoryMergeRequestStates(
	ctx context.Context,
	tx pgx.Tx,
	repositoryID string,
	query RepositoryMergeRequestQuery,
) (int64, int64, int64, error) {
	builder := buildRepositoryMergeRequestFilter(repositoryID, query, false)
	var openCount, closedCount, mergedCount int64
	err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE mr.state = 'open'),
		       COUNT(*) FILTER (WHERE mr.state = 'closed'),
		       COUNT(*) FILTER (WHERE mr.state = 'merged')
`+repositoryMergeRequestListFrom+builder.where(), builder.arguments...).Scan(
		&openCount, &closedCount, &mergedCount,
	)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("count repository pull request states: %w", err)
	}
	return openCount, closedCount, mergedCount, nil
}

func listRepositoryMergeRequests(
	ctx context.Context,
	tx pgx.Tx,
	repositoryID string,
	query RepositoryMergeRequestQuery,
) ([]MergeRequestListItem, error) {
	builder := buildRepositoryMergeRequestFilter(repositoryID, query, true)
	limit := builder.bind(query.PerPage)
	offset := builder.bind((query.Page - 1) * query.PerPage)
	rows, err := tx.Query(ctx, `
		SELECT mr.id, mr.number, mr.title, mr.body, mr.state, mr.is_draft,
		       mr.source_branch, mr.target_branch, mr.source_revision, mr.target_revision,
		       author.username, mr.author_id,
		       (SELECT COUNT(*) FROM merge_request_reviews review
		        WHERE review.merge_request_id = mr.id
		          AND review.source_revision = mr.source_revision
		          AND review.decision = 'approved'),
		       merged.username, mr.merged_revision, mr.merged_at,
		       COALESCE((
		           SELECT jsonb_agg(jsonb_build_object(
		               'id', label.id,
		               'repositoryId', label.repository_id,
		               'name', label.name,
		               'description', label.description,
		               'color', label.color,
		               'createdAt', label.created_at
		           ) ORDER BY lower(label.name), label.id)
		           FROM merge_request_labels assignment
		           JOIN labels label ON label.id = assignment.label_id
		           WHERE assignment.merge_request_id = mr.id
		       ), '[]'::jsonb),
		       COALESCE((
		           SELECT jsonb_agg(jsonb_build_object(
		               'id', assigned_user.id,
		               'username', assigned_user.username,
		               'displayName', assigned_user.display_name,
		               'avatarUrl', assigned_user.avatar_url
		           ) ORDER BY assignment.assigned_at, assigned_user.username)
		           FROM merge_request_assignees assignment
		           JOIN users assigned_user ON assigned_user.id = assignment.user_id
		           WHERE assignment.merge_request_id = mr.id
		       ), '[]'::jsonb),
		       milestone.id, milestone.number, milestone.title,
		       milestone.state, to_char(milestone.due_on, 'YYYY-MM-DD'),
		       mr.comment_count AS comment_count,
		       mr.created_at, mr.updated_at, mr.closed_at
`+repositoryMergeRequestListFrom+builder.where()+`
		ORDER BY `+repositoryWorkItemOrder("mr", query.Sort, query.Direction)+`
		LIMIT `+limit+` OFFSET `+offset,
		builder.arguments...,
	)
	if err != nil {
		return nil, fmt.Errorf("list repository pull requests: %w", err)
	}
	defer rows.Close()
	mergeRequests := make([]MergeRequestListItem, 0)
	for rows.Next() {
		mergeRequest, err := scanRepositoryMergeRequest(rows)
		if err != nil {
			return nil, err
		}
		mergeRequests = append(mergeRequests, mergeRequest)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate repository pull requests: %w", err)
	}
	return mergeRequests, nil
}

func scanRepositoryMergeRequest(row pgx.Row) (MergeRequestListItem, error) {
	var item MergeRequestListItem
	var labels, assignees json.RawMessage
	var milestoneID, milestoneTitle, milestoneState, milestoneDueOn *string
	var milestoneNumber *int64
	err := row.Scan(
		&item.ID, &item.Number, &item.Title, &item.Body, &item.State, &item.IsDraft,
		&item.SourceBranch, &item.TargetBranch, &item.SourceRevision, &item.TargetRevision,
		&item.Author, &item.AuthorID, &item.ApprovalCount,
		&item.MergedBy, &item.MergedRevision, &item.MergedAt,
		&labels, &assignees,
		&milestoneID, &milestoneNumber, &milestoneTitle, &milestoneState, &milestoneDueOn,
		&item.CommentCount, &item.CreatedAt, &item.UpdatedAt, &item.ClosedAt,
	)
	if err != nil {
		return MergeRequestListItem{}, fmt.Errorf("scan repository pull request: %w", err)
	}
	if err := json.Unmarshal(labels, &item.Labels); err != nil {
		return MergeRequestListItem{}, fmt.Errorf("decode repository pull request labels: %w", err)
	}
	if err := json.Unmarshal(assignees, &item.Assignees); err != nil {
		return MergeRequestListItem{}, fmt.Errorf("decode repository pull request assignees: %w", err)
	}
	if milestoneID != nil && milestoneNumber != nil && milestoneTitle != nil && milestoneState != nil {
		item.Milestone = &MilestoneSummary{
			ID: *milestoneID, Number: *milestoneNumber, Title: *milestoneTitle,
			State: *milestoneState, DueOn: milestoneDueOn,
		}
	}
	return item, nil
}

func buildRepositoryMergeRequestFilter(
	repositoryID string,
	query RepositoryMergeRequestQuery,
	includeState bool,
) *repositoryQueryBuilder {
	builder := newRepositoryQueryBuilder(repositoryID)
	if includeState && query.State != "all" {
		builder.add("mr.state = " + builder.bind(query.State))
	}
	if query.Search != "" {
		pattern := builder.bind(repositorySearchPattern(query.Search))
		builder.add(`(mr.title || ' ' || mr.body) ILIKE ` + pattern + ` ESCAPE '\'`)
	}
	if query.Author != "" {
		builder.add("lower(author.username) = lower(" + builder.bind(query.Author) + ")")
	}
	addRepositoryMergeRequestAssigneeFilter(builder, query.Assignee)
	for _, label := range query.Labels {
		parameter := builder.bind(label)
		builder.add(`EXISTS (
			SELECT 1 FROM merge_request_labels filtered_assignment
			JOIN labels filtered_label ON filtered_label.id = filtered_assignment.label_id
			WHERE filtered_assignment.merge_request_id = mr.id
			  AND lower(filtered_label.name) = lower(` + parameter + `)
		)`)
	}
	if query.WithoutMilestone {
		builder.add("mr.milestone_id IS NULL")
	} else if query.MilestoneNumber != nil {
		builder.add("milestone.number = " + builder.bind(*query.MilestoneNumber))
	}
	if query.SourceBranch != "" {
		builder.add("mr.source_branch = " + builder.bind(query.SourceBranch))
	}
	if query.TargetBranch != "" {
		builder.add("mr.target_branch = " + builder.bind(query.TargetBranch))
	}
	if query.Draft != nil {
		builder.add("mr.is_draft = " + builder.bind(*query.Draft))
	}
	return builder
}

func addRepositoryMergeRequestAssigneeFilter(builder *repositoryQueryBuilder, assignee string) {
	if assignee == "" {
		return
	}
	if strings.EqualFold(assignee, "none") {
		builder.add("NOT EXISTS (SELECT 1 FROM merge_request_assignees WHERE merge_request_id = mr.id)")
		return
	}
	parameter := builder.bind(assignee)
	builder.add(`EXISTS (
		SELECT 1 FROM merge_request_assignees filtered_assignment
		JOIN users filtered_user ON filtered_user.id = filtered_assignment.user_id
		WHERE filtered_assignment.merge_request_id = mr.id
		  AND lower(filtered_user.username) = lower(` + parameter + `)
	)`)
}
