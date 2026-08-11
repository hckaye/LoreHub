package platform

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	WorkItemKindIssue       = "issue"
	WorkItemKindPullRequest = "pull_request"
)

type GlobalWorkItemFilter struct {
	State  string
	Scope  string
	Query  string
	Cursor string
	Limit  int
}

type GlobalWorkItemPage struct {
	Items      []GlobalWorkItem `json:"items"`
	NextCursor string           `json:"nextCursor,omitempty"`
}

type GlobalWorkItem struct {
	ID            string                   `json:"id"`
	Kind          string                   `json:"kind"`
	Repository    GlobalWorkItemRepository `json:"repository"`
	Number        int64                    `json:"number"`
	Title         string                   `json:"title"`
	State         string                   `json:"state"`
	IsDraft       bool                     `json:"isDraft"`
	Author        GlobalWorkItemUser       `json:"author"`
	Assignees     []GlobalWorkItemUser     `json:"assignees"`
	Labels        []GlobalWorkItemLabel    `json:"labels"`
	Milestone     *GlobalWorkItemMilestone `json:"milestone"`
	CommentCount  int64                    `json:"commentCount"`
	ApprovalCount int64                    `json:"approvalCount"`
	SourceBranch  string                   `json:"sourceBranch,omitempty"`
	TargetBranch  string                   `json:"targetBranch,omitempty"`
	CreatedAt     time.Time                `json:"createdAt"`
	UpdatedAt     time.Time                `json:"updatedAt"`
}

type GlobalWorkItemRepository struct {
	ID          string `json:"id"`
	Owner       string `json:"owner"`
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
}

type GlobalWorkItemUser struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	AvatarURL   string `json:"avatarUrl"`
}

type GlobalWorkItemLabel struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type GlobalWorkItemMilestone struct {
	Number int64  `json:"number"`
	Title  string `json:"title"`
}

type globalWorkItemCursor struct {
	UpdatedAt time.Time `json:"updatedAt"`
	ID        string    `json:"id"`
}

func (store *Store) ListGlobalIssues(
	ctx context.Context,
	actor User,
	filter GlobalWorkItemFilter,
) (GlobalWorkItemPage, error) {
	filter, cursorTime, cursorID, err := normalizeGlobalWorkItemFilter(WorkItemKindIssue, filter)
	if err != nil {
		return GlobalWorkItemPage{}, err
	}
	rows, err := store.pool.Query(ctx, globalIssueQuery, actor.ID, filter.Scope, filter.State,
		filter.Query, globalWorkItemPattern(filter.Query), cursorTime, cursorID, filter.Limit+1)
	if err != nil {
		return GlobalWorkItemPage{}, fmt.Errorf("list global issues: %w", err)
	}
	defer rows.Close()
	return scanGlobalWorkItems(rows, filter.Limit)
}

func (store *Store) ListGlobalPullRequests(
	ctx context.Context,
	actor User,
	filter GlobalWorkItemFilter,
) (GlobalWorkItemPage, error) {
	filter, cursorTime, cursorID, err := normalizeGlobalWorkItemFilter(WorkItemKindPullRequest, filter)
	if err != nil {
		return GlobalWorkItemPage{}, err
	}
	rows, err := store.pool.Query(ctx, globalPullRequestQuery, actor.ID, filter.Scope, filter.State,
		filter.Query, globalWorkItemPattern(filter.Query), cursorTime, cursorID, filter.Limit+1)
	if err != nil {
		return GlobalWorkItemPage{}, fmt.Errorf("list global pull requests: %w", err)
	}
	defer rows.Close()
	return scanGlobalWorkItems(rows, filter.Limit)
}

func normalizeGlobalWorkItemFilter(
	kind string,
	filter GlobalWorkItemFilter,
) (GlobalWorkItemFilter, *time.Time, *string, error) {
	filter.Query = strings.TrimSpace(filter.Query)
	if len([]rune(filter.Query)) > 160 {
		return filter, nil, nil, ErrInvalidInput
	}
	if filter.Limit == 0 {
		filter.Limit = 25
	}
	if filter.Limit < 1 || filter.Limit > 100 {
		return filter, nil, nil, ErrInvalidInput
	}
	validStates := map[string]bool{"all": true, "open": true, "closed": true}
	validScopes := map[string]bool{"all": true, "involved": true, "created": true, "assigned": true}
	if kind == WorkItemKindPullRequest {
		validStates["merged"] = true
		validScopes["review_requested"] = true
	}
	if !validStates[filter.State] || !validScopes[filter.Scope] {
		return filter, nil, nil, ErrInvalidInput
	}
	if filter.Cursor == "" {
		return filter, nil, nil, nil
	}
	cursor, err := decodeGlobalWorkItemCursor(filter.Cursor)
	if err != nil {
		return filter, nil, nil, ErrInvalidInput
	}
	return filter, &cursor.UpdatedAt, &cursor.ID, nil
}

func decodeGlobalWorkItemCursor(value string) (globalWorkItemCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) > 512 {
		return globalWorkItemCursor{}, errors.New("invalid cursor encoding")
	}
	var cursor globalWorkItemCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil || cursor.UpdatedAt.IsZero() {
		return globalWorkItemCursor{}, errors.New("invalid cursor payload")
	}
	if _, err := uuid.Parse(cursor.ID); err != nil {
		return globalWorkItemCursor{}, errors.New("invalid cursor id")
	}
	return cursor, nil
}

func globalWorkItemPattern(query string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
	return "%" + escaped + "%"
}

func encodeGlobalWorkItemCursor(item GlobalWorkItem) (string, error) {
	encoded, err := json.Marshal(globalWorkItemCursor{UpdatedAt: item.UpdatedAt, ID: item.ID})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func scanGlobalWorkItems(rows pgx.Rows, limit int) (GlobalWorkItemPage, error) {
	items := make([]GlobalWorkItem, 0, limit+1)
	for rows.Next() {
		item, err := scanGlobalWorkItem(rows)
		if err != nil {
			return GlobalWorkItemPage{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return GlobalWorkItemPage{}, fmt.Errorf("iterate global work items: %w", err)
	}
	page := GlobalWorkItemPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	nextCursor, err := encodeGlobalWorkItemCursor(page.Items[len(page.Items)-1])
	if err != nil {
		return GlobalWorkItemPage{}, fmt.Errorf("encode global work item cursor: %w", err)
	}
	page.NextCursor = nextCursor
	return page, nil
}

func scanGlobalWorkItem(row pgx.Row) (GlobalWorkItem, error) {
	var item GlobalWorkItem
	var assigneesJSON, labelsJSON []byte
	var milestoneNumber *int64
	var milestoneTitle *string
	err := row.Scan(
		&item.ID, &item.Kind,
		&item.Repository.ID, &item.Repository.Owner, &item.Repository.Slug,
		&item.Repository.DisplayName, &item.Number, &item.Title, &item.State, &item.IsDraft,
		&item.Author.ID, &item.Author.Username, &item.Author.DisplayName, &item.Author.AvatarURL,
		&assigneesJSON, &labelsJSON, &milestoneNumber, &milestoneTitle,
		&item.CommentCount, &item.ApprovalCount, &item.SourceBranch, &item.TargetBranch,
		&item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return GlobalWorkItem{}, fmt.Errorf("scan global work item: %w", err)
	}
	if err := json.Unmarshal(assigneesJSON, &item.Assignees); err != nil {
		return GlobalWorkItem{}, fmt.Errorf("decode global work item assignees: %w", err)
	}
	if err := json.Unmarshal(labelsJSON, &item.Labels); err != nil {
		return GlobalWorkItem{}, fmt.Errorf("decode global work item labels: %w", err)
	}
	if milestoneNumber != nil && milestoneTitle != nil {
		item.Milestone = &GlobalWorkItemMilestone{Number: *milestoneNumber, Title: *milestoneTitle}
	}
	return item, nil
}

const globalWorkItemUserJSON = `jsonb_build_object(
	'id', item_user.id,
	'username', item_user.username,
	'displayName', item_user.display_name,
	'avatarUrl', item_user.avatar_url
)`

const globalWorkItemLabelJSON = `jsonb_build_object(
	'id', item_label.id,
	'name', item_label.name,
	'color', item_label.color
)`

var globalIssueQuery = `
	SELECT issue.id, 'issue', repository.id, organization.slug, repository.slug,
	       repository.display_name, issue.number, issue.title, issue.state, false,
	       author.id, author.username, author.display_name, author.avatar_url,
	       COALESCE((
	           SELECT jsonb_agg(` + globalWorkItemUserJSON + ` ORDER BY assignment.assigned_at, item_user.id)
	           FROM issue_assignees assignment
	           JOIN users item_user ON item_user.id = assignment.user_id AND item_user.status = 'active'
	           WHERE assignment.issue_id = issue.id
	       ), '[]'::jsonb),
	       COALESCE((
	           SELECT jsonb_agg(` + globalWorkItemLabelJSON + ` ORDER BY item_label.name, item_label.id)
	           FROM issue_labels applied_label
	           JOIN labels item_label ON item_label.id = applied_label.label_id
	           WHERE applied_label.issue_id = issue.id
	       ), '[]'::jsonb),
	       milestone.number, milestone.title,
	       (SELECT COUNT(*) FROM issue_comments comment WHERE comment.issue_id = issue.id),
	       0, '', '', issue.created_at, issue.updated_at
	FROM issues issue
	JOIN repositories repository ON repository.id = issue.repository_id
	JOIN organizations organization ON organization.id = repository.organization_id
	JOIN users author ON author.id = issue.author_id
	LEFT JOIN repository_milestones milestone ON milestone.id = issue.milestone_id
	WHERE ` + repositoryAccessClause("repository", "$1") + `
	  AND ($3 = 'all' OR issue.state = $3)
	  AND (
	      $4 = ''
	      OR to_tsvector('simple', issue.title || ' ' || issue.body)
	         @@ websearch_to_tsquery('simple', $4)
	      OR (issue.title || ' ' || issue.body) ILIKE $5 ESCAPE '\'
	  )
	  AND (
	      $6::timestamptz IS NULL
	      OR (issue.updated_at, issue.id) < ($6::timestamptz, $7::uuid)
	  )
	  AND (
	      $2 = 'all'
	      OR $2 = 'created' AND issue.author_id = $1::uuid
	      OR $2 = 'assigned' AND EXISTS (
	          SELECT 1 FROM issue_assignees assignment
	          WHERE assignment.issue_id = issue.id AND assignment.user_id = $1::uuid
	      )
	      OR $2 = 'involved' AND (
	          issue.author_id = $1::uuid
	          OR EXISTS (
	              SELECT 1 FROM issue_assignees assignment
	              WHERE assignment.issue_id = issue.id AND assignment.user_id = $1::uuid
	          )
	      )
	  )
	ORDER BY issue.updated_at DESC, issue.id DESC
	LIMIT $8
`

var globalPullRequestQuery = `
	SELECT request.id, 'pull_request', repository.id, organization.slug, repository.slug,
	       repository.display_name, request.number, request.title, request.state, request.is_draft,
	       author.id, author.username, author.display_name, author.avatar_url,
	       COALESCE((
	           SELECT jsonb_agg(` + globalWorkItemUserJSON + ` ORDER BY assignment.assigned_at, item_user.id)
	           FROM merge_request_assignees assignment
	           JOIN users item_user ON item_user.id = assignment.user_id AND item_user.status = 'active'
	           WHERE assignment.merge_request_id = request.id
	       ), '[]'::jsonb),
	       COALESCE((
	           SELECT jsonb_agg(` + globalWorkItemLabelJSON + ` ORDER BY item_label.name, item_label.id)
	           FROM merge_request_labels applied_label
	           JOIN labels item_label ON item_label.id = applied_label.label_id
	           WHERE applied_label.merge_request_id = request.id
	       ), '[]'::jsonb),
	       milestone.number, milestone.title,
	       (SELECT COUNT(*) FROM merge_request_comments comment
	        WHERE comment.merge_request_id = request.id),
	       (SELECT COUNT(*) FROM merge_request_reviews review
	        WHERE review.merge_request_id = request.id
	          AND review.source_revision = request.source_revision
	          AND review.decision = 'approved'),
	       request.source_branch, request.target_branch, request.created_at, request.updated_at
	FROM merge_requests request
	JOIN repositories repository ON repository.id = request.repository_id
	JOIN organizations organization ON organization.id = repository.organization_id
	JOIN users author ON author.id = request.author_id
	LEFT JOIN repository_milestones milestone ON milestone.id = request.milestone_id
	WHERE ` + repositoryAccessClause("repository", "$1") + `
	  AND ($3 = 'all' OR request.state = $3)
	  AND (
	      $4 = ''
	      OR to_tsvector('simple', request.title || ' ' || request.body)
	         @@ websearch_to_tsquery('simple', $4)
	      OR (request.title || ' ' || request.body) ILIKE $5 ESCAPE '\'
	  )
	  AND (
	      $6::timestamptz IS NULL
	      OR (request.updated_at, request.id) < ($6::timestamptz, $7::uuid)
	  )
	  AND (
	      $2 = 'all'
	      OR $2 = 'created' AND request.author_id = $1::uuid
	      OR $2 = 'assigned' AND EXISTS (
	          SELECT 1 FROM merge_request_assignees assignment
	          WHERE assignment.merge_request_id = request.id AND assignment.user_id = $1::uuid
	      )
	      OR $2 IN ('involved', 'review_requested') AND ` + globalReviewRequestedClause + `
	      OR $2 = 'involved' AND (
	          request.author_id = $1::uuid
	          OR EXISTS (
	              SELECT 1 FROM merge_request_assignees assignment
	              WHERE assignment.merge_request_id = request.id AND assignment.user_id = $1::uuid
	          )
	      )
	  )
	ORDER BY request.updated_at DESC, request.id DESC
	LIMIT $8
`

const globalReviewRequestedClause = `EXISTS (
	SELECT 1
	FROM merge_request_review_requests review_request
	WHERE review_request.merge_request_id = request.id
	  AND review_request.removed_at IS NULL
	  AND (
	      review_request.reviewer_user_id = $1::uuid
	      OR EXISTS (
	          SELECT 1
	          FROM teams review_team
	          JOIN team_memberships team_member
	            ON team_member.team_id = review_team.id
	           AND team_member.user_id = $1::uuid
	           AND team_member.active
	          JOIN organization_memberships org_member
	            ON org_member.organization_id = review_team.organization_id
	           AND org_member.user_id = team_member.user_id
	           AND org_member.active
	          WHERE review_team.id = review_request.reviewer_team_id
	            AND review_team.active
	      )
	  )
)`
