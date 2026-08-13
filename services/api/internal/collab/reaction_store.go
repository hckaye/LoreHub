package collab

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

const (
	reactionIssue               = "issue"
	reactionMergeRequest        = "merge_request"
	reactionIssueComment        = "issue_comment"
	reactionMergeRequestComment = "merge_request_comment"
)

var (
	ErrInvalidReaction        = errors.New("reaction is invalid")
	ErrInvalidReactionSubject = errors.New("reaction subject is invalid")
)

var allowedReactions = map[string]struct{}{
	"+1": {}, "-1": {}, "laugh": {}, "confused": {},
	"heart": {}, "hooray": {}, "rocket": {}, "eyes": {},
}

// Reaction is one aggregate row for a subject. ViewerReacted is scoped to the
// authenticated reader used to build the response.
type Reaction struct {
	Reaction      string `json:"reaction"`
	Count         int64  `json:"count"`
	ViewerReacted bool   `json:"viewerReacted"`
}

// ReactionInput identifies an issue, pull request, or comment reaction.
type ReactionInput struct {
	SubjectKind string `json:"subjectKind"`
	SubjectID   string `json:"subjectId"`
	Reaction    string `json:"reaction"`
}

type ReactionMutation struct {
	Reactions []Reaction `json:"reactions"`
}

// ReactionStore is the write contract for repository reactions.
type ReactionStore interface {
	PutReaction(context.Context, platform.User, string, ReactionInput) (ReactionMutation, error)
	DeleteReaction(context.Context, platform.User, string, ReactionInput) (ReactionMutation, error)
}

// ReactionReadStore enriches the existing issue, pull request, and comment
// projections with grouped reaction counts for a named viewer.
type ReactionReadStore interface {
	GetIssueWithReactions(context.Context, string, int64, string) (Issue, error)
	ListIssueCommentsWithReactions(context.Context, string, int64, Page, string) (Result[IssueComment], error)
	GetMergeRequestWithReactions(context.Context, string, int64, string) (MergeRequest, error)
	ListMergeRequestCommentsWithReactions(
		context.Context, string, int64, Page, string,
	) (Result[MergeRequestComment], error)
	ListIssuesForRepositoryWithReactions(
		context.Context, string, RepositoryIssueQuery, string,
	) (RepositoryIssuePage, error)
	ListMergeRequestsForRepositoryWithReactions(
		context.Context, string, RepositoryMergeRequestQuery, string,
	) (RepositoryMergeRequestPage, error)
}

func normalizeReactionInput(input ReactionInput) (ReactionInput, error) {
	input.SubjectKind = strings.TrimSpace(input.SubjectKind)
	input.SubjectID = strings.TrimSpace(input.SubjectID)
	input.Reaction = strings.TrimSpace(input.Reaction)
	switch input.SubjectKind {
	case reactionIssue, reactionMergeRequest, reactionIssueComment, reactionMergeRequestComment:
	default:
		return ReactionInput{}, ErrInvalidReactionSubject
	}
	if _, err := uuid.Parse(input.SubjectID); err != nil {
		return ReactionInput{}, ErrInvalidReactionSubject
	}
	if _, ok := allowedReactions[input.Reaction]; !ok {
		return ReactionInput{}, ErrInvalidReaction
	}
	return input, nil
}

func (s *store) PutReaction(
	ctx context.Context,
	actor platform.User,
	repositoryID string,
	input ReactionInput,
) (ReactionMutation, error) {
	return s.mutateReaction(ctx, actor, repositoryID, input, true)
}

func (s *store) DeleteReaction(
	ctx context.Context,
	actor platform.User,
	repositoryID string,
	input ReactionInput,
) (ReactionMutation, error) {
	return s.mutateReaction(ctx, actor, repositoryID, input, false)
}

func (s *store) mutateReaction(
	ctx context.Context,
	actor platform.User,
	repositoryID string,
	input ReactionInput,
	enabled bool,
) (ReactionMutation, error) {
	input, err := normalizeReactionInput(input)
	if err != nil {
		return ReactionMutation{}, err
	}
	organizationID, err := s.repoOrgID(ctx, repositoryID)
	if err != nil {
		return ReactionMutation{}, err
	}
	access, err := s.permFromRef(ctx, actor, repositoryID, organizationID)
	if err != nil {
		return ReactionMutation{}, err
	}
	if !access.AtLeast(PermRead) {
		return ReactionMutation{}, platform.ErrForbidden
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ReactionMutation{}, fmt.Errorf("begin reaction mutation: %w", err)
	}
	defer rollback(ctx, tx)
	if err := reactionSubjectExists(ctx, tx, repositoryID, input); err != nil {
		return ReactionMutation{}, err
	}
	if enabled {
		_, err = tx.Exec(ctx, `
			INSERT INTO comment_reactions (
				id, repository_id, subject_kind, subject_id, username, reaction
			)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (subject_kind, subject_id, username, reaction) DO NOTHING
		`, uuidArg(), repositoryID, input.SubjectKind, input.SubjectID, actor.Username, input.Reaction)
	} else {
		_, err = tx.Exec(ctx, `
			DELETE FROM comment_reactions
			WHERE repository_id = $1 AND subject_kind = $2 AND subject_id = $3
			  AND username = $4 AND reaction = $5
		`, repositoryID, input.SubjectKind, input.SubjectID, actor.Username, input.Reaction)
	}
	if err != nil {
		return ReactionMutation{}, translateConstraintError("mutate reaction", err)
	}
	reactions, err := loadReactions(
		ctx, tx, repositoryID, input.SubjectKind, []string{input.SubjectID}, actor.Username,
	)
	if err != nil {
		return ReactionMutation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ReactionMutation{}, fmt.Errorf("commit reaction mutation: %w", err)
	}
	return ReactionMutation{Reactions: reactions[input.SubjectID]}, nil
}

func reactionSubjectExists(
	ctx context.Context,
	tx pgx.Tx,
	repositoryID string,
	input ReactionInput,
) error {
	query := ""
	switch input.SubjectKind {
	case reactionIssue:
		query = `SELECT EXISTS (SELECT 1 FROM issues WHERE id = $1 AND repository_id = $2)`
	case reactionMergeRequest:
		query = `SELECT EXISTS (SELECT 1 FROM merge_requests WHERE id = $1 AND repository_id = $2)`
	case reactionIssueComment:
		query = `SELECT EXISTS (
			SELECT 1 FROM issue_comments comment
			JOIN issues issue ON issue.id = comment.issue_id
			WHERE comment.id = $1 AND issue.repository_id = $2
		)`
	case reactionMergeRequestComment:
		query = `SELECT EXISTS (
			SELECT 1 FROM merge_request_comments comment
			JOIN merge_requests merge_request ON merge_request.id = comment.merge_request_id
			WHERE comment.id = $1 AND merge_request.repository_id = $2
		)`
	}
	var exists bool
	if err := tx.QueryRow(ctx, query, input.SubjectID, repositoryID).Scan(&exists); err != nil {
		return fmt.Errorf("check reaction subject: %w", err)
	}
	if !exists {
		return platform.ErrNotFound
	}
	return nil
}

type reactionQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func loadReactions(
	ctx context.Context,
	queryer reactionQueryer,
	repositoryID string,
	subjectKind string,
	subjectIDs []string,
	viewerUsername string,
) (map[string][]Reaction, error) {
	result := make(map[string][]Reaction, len(subjectIDs))
	if len(subjectIDs) == 0 {
		return result, nil
	}
	for _, subjectID := range subjectIDs {
		result[subjectID] = make([]Reaction, 0)
	}
	parsedSubjectIDs := make([]uuid.UUID, 0, len(subjectIDs))
	for _, subjectID := range subjectIDs {
		parsed, err := uuid.Parse(subjectID)
		if err != nil {
			return nil, fmt.Errorf("parse reaction subject id: %w", err)
		}
		parsedSubjectIDs = append(parsedSubjectIDs, parsed)
	}
	rows, err := queryer.Query(ctx, `
		SELECT subject_id::text, reaction, COUNT(*)::bigint,
		       bool_or(username = $4)
		FROM comment_reactions
		WHERE repository_id = $1 AND subject_kind = $2
		  AND subject_id = ANY($3::uuid[])
		GROUP BY subject_id, reaction
		ORDER BY subject_id, reaction
	`, repositoryID, subjectKind, parsedSubjectIDs, viewerUsername)
	if err != nil {
		return nil, fmt.Errorf("aggregate reactions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var subjectID string
		var reaction Reaction
		if err := rows.Scan(&subjectID, &reaction.Reaction, &reaction.Count, &reaction.ViewerReacted); err != nil {
			return nil, fmt.Errorf("scan reaction aggregate: %w", err)
		}
		result[subjectID] = append(result[subjectID], reaction)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reaction aggregates: %w", err)
	}
	return result, nil
}
