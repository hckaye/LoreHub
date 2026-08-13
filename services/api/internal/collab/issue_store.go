package collab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

const issueDetailQuery = `
	SELECT i.id, i.number, i.title, i.body, i.state,
	       author.username, i.author_id,
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
	       milestone.id, milestone.number, milestone.title, milestone.state,
	       to_char(milestone.due_on, 'YYYY-MM-DD'),
	       COALESCE((
	           SELECT jsonb_agg(jsonb_build_object(
	               'id', label.id,
	               'repositoryId', label.repository_id,
	               'name', label.name,
	               'description', label.description,
	               'color', label.color,
	               'createdAt', label.created_at
	           ) ORDER BY label.name, label.id)
	           FROM issue_labels issue_label
	           JOIN labels label ON label.id = issue_label.label_id
	           WHERE issue_label.issue_id = i.id
	       ), '[]'::jsonb),
	       i.comment_count,
	       i.created_at, i.updated_at, closed_by.username, i.closed_at
	FROM issues i
	JOIN users author ON author.id = i.author_id
	LEFT JOIN repository_milestones milestone ON milestone.id = i.milestone_id
	LEFT JOIN users closed_by ON closed_by.id = i.closed_by
	WHERE i.repository_id = $1 AND i.number = $2
`

// GetIssue loads an issue by repository and number. It does not perform any
// visibility check; callers must have already resolved a visible repository.
func (s *store) GetIssue(ctx context.Context, repoID string, number int64) (Issue, error) {
	row := s.pool.QueryRow(ctx, issueDetailQuery, repoID, number)
	issue, err := scanIssue(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Issue{}, platform.ErrNotFound
	}
	if err != nil {
		return Issue{}, fmt.Errorf("get issue: %w", err)
	}
	return issue, nil
}

func (s *store) GetIssueWithReactions(
	ctx context.Context,
	repoID string,
	number int64,
	viewerUsername string,
) (Issue, error) {
	issue, err := s.GetIssue(ctx, repoID, number)
	if err != nil {
		return Issue{}, err
	}
	reactions, err := loadReactions(
		ctx, s.pool, repoID, reactionIssue, []string{issue.ID}, viewerUsername,
	)
	if err != nil {
		return Issue{}, err
	}
	issue.Reactions = reactions[issue.ID]
	return issue, nil
}

func scanIssue(row pgx.Row) (Issue, error) {
	var issue Issue
	var assignees, labels json.RawMessage
	var milestoneID, milestoneTitle, milestoneState, milestoneDueOn *string
	var milestoneNumber *int64
	err := row.Scan(
		&issue.ID,
		&issue.Number,
		&issue.Title,
		&issue.Body,
		&issue.State,
		&issue.Author,
		&issue.AuthorID,
		&assignees,
		&milestoneID,
		&milestoneNumber,
		&milestoneTitle,
		&milestoneState,
		&milestoneDueOn,
		&labels,
		&issue.CommentCount,
		&issue.CreatedAt,
		&issue.UpdatedAt,
		&issue.ClosedBy,
		&issue.ClosedAt,
	)
	if err != nil {
		return Issue{}, err
	}
	if err := json.Unmarshal(assignees, &issue.Assignees); err != nil {
		return Issue{}, fmt.Errorf("decode issue assignees: %w", err)
	}
	setPrimaryAssignee(&issue)
	if err := json.Unmarshal(labels, &issue.Labels); err != nil {
		return Issue{}, fmt.Errorf("decode issue labels: %w", err)
	}
	issue.LabelCount = int64(len(issue.Labels))
	if milestoneID != nil && milestoneNumber != nil && milestoneTitle != nil && milestoneState != nil {
		issue.Milestone = &MilestoneSummary{
			ID: *milestoneID, Number: *milestoneNumber, Title: *milestoneTitle,
			State: *milestoneState, DueOn: milestoneDueOn,
		}
	}
	return issue, nil
}

func setPrimaryAssignee(issue *Issue) {
	if len(issue.Assignees) > 0 {
		issue.Assignee = &issue.Assignees[0].Username
	}
}

// UpdateIssue applies a partial update to an issue. When IfMatch is set, the
// update is conditional on the stored updated_at matching, providing optimistic
// concurrency; a mismatch returns ErrPreconditionFailed. The author or a
// triage+ actor may update; insufficient permission returns ErrForbidden.
func (s *store) UpdateIssue(
	ctx context.Context,
	actor platform.User,
	repoID string,
	number int64,
	input UpdateIssueInput,
) (Issue, error) {
	allowed, orgID, err := s.checkIssueMutation(ctx, actor, repoID, number)
	if err != nil {
		return Issue{}, err
	}
	if !allowed {
		return Issue{}, platform.ErrForbidden
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Issue{}, fmt.Errorf("begin issue update: %w", err)
	}
	defer rollback(ctx, tx)

	now := nowUTC()
	query, args, err := buildIssueUpdateQuery(repoID, number, actor.ID, input, now)
	if err != nil {
		return Issue{}, err
	}
	tag, err := tx.Exec(ctx, query, args...)
	if err != nil {
		return Issue{}, translateConstraintError("update issue", err)
	}
	if tag.RowsAffected() == 0 {
		issue, lookupErr := s.GetIssue(ctx, repoID, number)
		if lookupErr != nil {
			return Issue{}, lookupErr
		}
		if input.IfMatch != nil && !issue.UpdatedAt.Equal(*input.IfMatch) {
			return Issue{}, ErrPreconditionFailed
		}
		return Issue{}, platform.ErrNotFound
	}

	issue, err := scanIssueByTx(ctx, tx, repoID, number)
	if err != nil {
		return Issue{}, err
	}
	action := "issue.update"
	if input.State != nil {
		switch *input.State {
		case "closed":
			action = "issue.close"
		case "open":
			action = "issue.reopen"
		}
	}
	if err := insertAudit(ctx, tx, actor.ID, orgID, repoID, action, "issue", issue.ID); err != nil {
		return Issue{}, err
	}
	if err := insertOutbox(ctx, tx, "issue.updated", issue.ID+":"+uuidArg(), issue); err != nil {
		return Issue{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Issue{}, fmt.Errorf("commit issue update: %w", err)
	}
	return issue, nil
}

// checkIssueMutation permits the issue author or an actor with triage+ access
// and returns the organization id.
func (s *store) checkIssueMutation(
	ctx context.Context,
	actor platform.User,
	repoID string,
	number int64,
) (bool, string, error) {
	var orgID, authorID string
	var archived bool
	err := s.pool.QueryRow(ctx, `
		SELECT r.organization_id, i.author_id, r.archived_at IS NOT NULL
		FROM issues i
		JOIN repositories r ON r.id = i.repository_id
		WHERE i.repository_id = $1 AND i.number = $2
	`, repoID, number).Scan(&orgID, &authorID, &archived)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, "", platform.ErrNotFound
	}
	if err != nil {
		return false, "", fmt.Errorf("find issue for mutation: %w", err)
	}
	if archived {
		return false, orgID, nil
	}
	if actor.ID == authorID {
		return true, orgID, nil
	}
	repo := Repository{ID: repoID, OrganizationID: orgID}
	access, err := s.RepositoryPermission(ctx, actor, repo)
	if err != nil {
		return false, "", err
	}
	return access.AtLeast(PermTriage), orgID, nil
}

func buildIssueUpdateQuery(
	repoID string,
	number int64,
	actorID string,
	input UpdateIssueInput,
	now time.Time,
) (string, []any, error) {
	sets := []string{"updated_at = $1"}
	args := []any{now}
	idx := 2
	if input.Title != nil {
		sets = append(sets, fmt.Sprintf("title = $%d", idx))
		args = append(args, *input.Title)
		idx++
	}
	if input.Body != nil {
		sets = append(sets, fmt.Sprintf("body = $%d", idx))
		args = append(args, *input.Body)
		idx++
	}
	if input.State != nil {
		state := *input.State
		switch state {
		case "closed":
			sets = append(sets,
				fmt.Sprintf("state = $%d", idx),
				fmt.Sprintf("closed_by = $%d", idx+1),
				"closed_at = $1",
			)
			args = append(args, state)
			idx++
			args = append(args, actorID)
			idx++
		case "open":
			sets = append(sets,
				fmt.Sprintf("state = $%d", idx), "closed_by = NULL", "closed_at = NULL",
			)
			args = append(args, state)
			idx++
		default:
			return "", nil, ErrInvalidState
		}
	}
	where := []string{fmt.Sprintf("repository_id = $%d", idx), fmt.Sprintf("number = $%d", idx+1)}
	args = append(args, repoID, number)
	if input.IfMatch != nil {
		where = append(where, fmt.Sprintf("updated_at = $%d", idx+2))
		args = append(args, *input.IfMatch)
	}
	query := fmt.Sprintf(
		"UPDATE issues SET %s WHERE %s",
		joinStrings(sets, ", "), joinStrings(where, " AND "),
	)
	return query, args, nil
}

func scanIssueByTx(ctx context.Context, tx pgx.Tx, repoID string, number int64) (Issue, error) {
	row := tx.QueryRow(ctx, issueDetailQuery, repoID, number)
	return scanIssue(row)
}

func rollback(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(context.WithoutCancel(ctx))
}

func joinStrings(items []string, separator string) string {
	if len(items) == 0 {
		return ""
	}
	out := items[0]
	for _, item := range items[1:] {
		out += separator + item
	}
	return out
}
