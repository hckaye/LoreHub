package collab

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

// GetIssue loads an issue by repository and number. It does not perform any
// visibility check; callers must have already resolved a visible repository.
func (s *store) GetIssue(ctx context.Context, repoID string, number int64) (Issue, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT i.id, i.number, i.title, i.body, i.state,
		       author.username, i.author_id,
		       assignee.username,
		       COUNT(DISTINCT il.label_id), COUNT(DISTINCT c.id),
		       i.created_at, i.updated_at, i.closed_at
		FROM issues i
		JOIN users author ON author.id = i.author_id
		LEFT JOIN users assignee ON assignee.id = i.assignee_id
		LEFT JOIN issue_labels il ON il.issue_id = i.id
		LEFT JOIN issue_comments c ON c.issue_id = i.id
		WHERE i.repository_id = $1 AND i.number = $2
		GROUP BY i.id, author.username, assignee.username
	`, repoID, number)
	issue, err := scanIssue(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Issue{}, platform.ErrNotFound
	}
	if err != nil {
		return Issue{}, fmt.Errorf("get issue: %w", err)
	}
	return issue, nil
}

func scanIssue(row pgx.Row) (Issue, error) {
	var issue Issue
	err := row.Scan(
		&issue.ID,
		&issue.Number,
		&issue.Title,
		&issue.Body,
		&issue.State,
		&issue.Author,
		&issue.AuthorID,
		&issue.Assignee,
		&issue.LabelCount,
		&issue.CommentCount,
		&issue.CreatedAt,
		&issue.UpdatedAt,
		&issue.ClosedAt,
	)
	return issue, err
}

// UpdateIssue applies a partial update to an issue. When IfMatch is set, the
// update is conditional on the stored updated_at matching, providing optimistic
// concurrency; a mismatch returns ErrPreconditionFailed. Only the author or a
// triage+ actor may update; insufficient permission returns ErrForbidden.
func (s *store) UpdateIssue(
	ctx context.Context,
	actor platform.User,
	repoID string,
	number int64,
	input UpdateIssueInput,
) (Issue, error) {
	allowed, authorID, orgID, err := s.checkIssueMutation(ctx, actor, repoID, number)
	if err != nil {
		return Issue{}, err
	}
	if !allowed && authorID != actor.ID {
		return Issue{}, platform.ErrForbidden
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Issue{}, fmt.Errorf("begin issue update: %w", err)
	}
	defer rollback(ctx, tx)

	now := nowUTC()
	query, args, err := buildIssueUpdateQuery(repoID, number, input, now)
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

// checkIssueMutation reports whether the actor has triage+ permission on the
// issue's repository and returns the issue author and organization id.
func (s *store) checkIssueMutation(
	ctx context.Context,
	actor platform.User,
	repoID string,
	number int64,
) (bool, string, string, error) {
	var authorID, orgID string
	err := s.pool.QueryRow(ctx, `
		SELECT i.author_id, r.organization_id
		FROM issues i
		JOIN repositories r ON r.id = i.repository_id
		WHERE i.repository_id = $1 AND i.number = $2
	`, repoID, number).Scan(&authorID, &orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, "", "", platform.ErrNotFound
	}
	if err != nil {
		return false, "", "", fmt.Errorf("find issue for mutation: %w", err)
	}
	repo := Repository{ID: repoID, OrganizationID: orgID}
	access, err := s.RepositoryPermission(ctx, actor, repo)
	if err != nil {
		return false, "", "", err
	}
	return access.AtLeast(PermTriage), authorID, orgID, nil
}

func buildIssueUpdateQuery(
	repoID string,
	number int64,
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
			sets = append(sets, fmt.Sprintf("state = $%d", idx), "closed_at = $1")
			args = append(args, state)
			idx++
		case "open":
			sets = append(sets, fmt.Sprintf("state = $%d", idx), "closed_at = NULL")
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
	row := tx.QueryRow(ctx, `
		SELECT i.id, i.number, i.title, i.body, i.state,
		       author.username, i.author_id,
		       assignee.username,
		       COUNT(DISTINCT il.label_id), COUNT(DISTINCT c.id),
		       i.created_at, i.updated_at, i.closed_at
		FROM issues i
		JOIN users author ON author.id = i.author_id
		LEFT JOIN users assignee ON assignee.id = i.assignee_id
		LEFT JOIN issue_labels il ON il.issue_id = i.id
		LEFT JOIN issue_comments c ON c.issue_id = i.id
		WHERE i.repository_id = $1 AND i.number = $2
		GROUP BY i.id, author.username, assignee.username
	`, repoID, number)
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
