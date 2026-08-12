package collab

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

const (
	defaultRevisionCommentPageSize = 30
	maxRevisionCommentPageSize     = 100
	maxRevisionCommentBodyBytes    = 1_000_000
)

func (s *store) ListRevisionComments(
	ctx context.Context,
	actor *platform.User,
	repository Repository,
	revision string,
	page int,
	perPage int,
) (RevisionCommentPage, error) {
	revision, err := validateRevisionCommentRevision(revision)
	if err != nil {
		return RevisionCommentPage{}, err
	}
	page, perPage, offset, err := normalizeRevisionCommentPage(page, perPage)
	if err != nil {
		return RevisionCommentPage{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return RevisionCommentPage{}, fmt.Errorf("begin revision comment list: %w", err)
	}
	defer rollback(ctx, tx)
	actorID := ""
	if actor != nil {
		actorID = actor.ID
	}
	permission, err := revisionCommentPermission(ctx, tx, actorID, repository, false)
	if err != nil {
		return RevisionCommentPage{}, err
	}
	var total int64
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM revision_comments comment
		WHERE comment.repository_id = $1 AND comment.revision = $2
	`, repository.ID, revision).Scan(&total); err != nil {
		return RevisionCommentPage{}, fmt.Errorf("count revision comments: %w", err)
	}
	rows, err := tx.Query(ctx, revisionCommentSelect+`
		WHERE comment.repository_id = $1 AND comment.revision = $2
		ORDER BY comment.created_at, comment.id
		LIMIT $3 OFFSET $4
	`, repository.ID, revision, perPage+1, offset)
	if err != nil {
		return RevisionCommentPage{}, fmt.Errorf("list revision comments: %w", err)
	}
	comments, err := scanRevisionComments(rows)
	rows.Close()
	if err != nil {
		return RevisionCommentPage{}, err
	}
	hasNext := len(comments) > perPage
	if hasNext {
		comments = comments[:perPage]
	}
	if actor != nil && repository.ArchivedAt == nil {
		for index := range comments {
			comments[index].ViewerCanUpdate = comments[index].Author.ID == actor.ID || permission >= PermTriage
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return RevisionCommentPage{}, fmt.Errorf("commit revision comment list: %w", err)
	}
	return RevisionCommentPage{
		Items: comments, Page: page, PerPage: perPage,
		TotalCount: total, HasNext: hasNext,
	}, nil
}

func (s *store) CreateRevisionComment(
	ctx context.Context,
	actor platform.User,
	repository Repository,
	revision string,
	body string,
) (RevisionComment, error) {
	revision, err := validateRevisionCommentRevision(revision)
	if err != nil {
		return RevisionComment{}, err
	}
	body, err = validateRevisionCommentBody(body)
	if err != nil {
		return RevisionComment{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return RevisionComment{}, fmt.Errorf("begin revision comment create: %w", err)
	}
	defer rollback(ctx, tx)
	if _, err := revisionCommentPermission(ctx, tx, actor.ID, repository, true); err != nil {
		return RevisionComment{}, err
	}
	comment := RevisionComment{
		ID: uuidArg(), Revision: revision,
		Author: RevisionCommentAuthor{
			ID: actor.ID, Username: actor.Username, DisplayName: actor.DisplayName,
		},
		Body: body, CreatedAt: nowUTC(), ViewerCanUpdate: true,
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO revision_comments (
			id, repository_id, revision, author_id, body, created_at
		) VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING (SELECT avatar_url FROM users WHERE id = $4)
	`, comment.ID, repository.ID, revision, actor.ID, body, comment.CreatedAt).
		Scan(&comment.Author.AvatarURL)
	if err != nil {
		return RevisionComment{}, translateRevisionCommentError("create revision comment", err)
	}
	if err := recordRevisionCommentEvent(
		ctx, tx, actor.ID, repository, "create", comment,
	); err != nil {
		return RevisionComment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RevisionComment{}, fmt.Errorf("commit revision comment create: %w", err)
	}
	return comment, nil
}

func (s *store) UpdateRevisionComment(
	ctx context.Context,
	actor platform.User,
	repository Repository,
	revision string,
	commentID string,
	body string,
) (RevisionComment, error) {
	revision, err := validateRevisionCommentRevision(revision)
	if err != nil {
		return RevisionComment{}, err
	}
	commentID, err = validateRevisionCommentID(commentID)
	if err != nil {
		return RevisionComment{}, err
	}
	body, err = validateRevisionCommentBody(body)
	if err != nil {
		return RevisionComment{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return RevisionComment{}, fmt.Errorf("begin revision comment update: %w", err)
	}
	defer rollback(ctx, tx)
	permission, err := revisionCommentPermission(ctx, tx, actor.ID, repository, true)
	if err != nil {
		return RevisionComment{}, err
	}
	comment, err := findRevisionComment(ctx, tx, repository.ID, revision, commentID, true)
	if err != nil {
		return RevisionComment{}, err
	}
	if comment.Author.ID != actor.ID && permission < PermTriage {
		return RevisionComment{}, platform.ErrForbidden
	}
	editedAt := nowUTC()
	tag, err := tx.Exec(ctx, `
		UPDATE revision_comments SET body = $4, edited_at = $5
		WHERE id = $1 AND repository_id = $2 AND revision = $3
	`, commentID, repository.ID, revision, body, editedAt)
	if err != nil {
		return RevisionComment{}, translateRevisionCommentError("update revision comment", err)
	}
	if tag.RowsAffected() != 1 {
		return RevisionComment{}, platform.ErrNotFound
	}
	comment.Body = body
	comment.EditedAt = &editedAt
	comment.ViewerCanUpdate = true
	if err := recordRevisionCommentEvent(
		ctx, tx, actor.ID, repository, "update", comment,
	); err != nil {
		return RevisionComment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RevisionComment{}, fmt.Errorf("commit revision comment update: %w", err)
	}
	return comment, nil
}

func (s *store) DeleteRevisionComment(
	ctx context.Context,
	actor platform.User,
	repository Repository,
	revision string,
	commentID string,
) error {
	revision, err := validateRevisionCommentRevision(revision)
	if err != nil {
		return err
	}
	commentID, err = validateRevisionCommentID(commentID)
	if err != nil {
		return err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin revision comment delete: %w", err)
	}
	defer rollback(ctx, tx)
	permission, err := revisionCommentPermission(ctx, tx, actor.ID, repository, true)
	if err != nil {
		return err
	}
	comment, err := findRevisionComment(ctx, tx, repository.ID, revision, commentID, true)
	if err != nil {
		return err
	}
	if comment.Author.ID != actor.ID && permission < PermTriage {
		return platform.ErrForbidden
	}
	tag, err := tx.Exec(ctx, `
		DELETE FROM revision_comments
		WHERE id = $1 AND repository_id = $2 AND revision = $3
	`, commentID, repository.ID, revision)
	if err != nil {
		return translateRevisionCommentError("delete revision comment", err)
	}
	if tag.RowsAffected() != 1 {
		return platform.ErrNotFound
	}
	if err := recordRevisionCommentEvent(
		ctx, tx, actor.ID, repository, "delete", comment,
	); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit revision comment delete: %w", err)
	}
	return nil
}

const revisionCommentSelect = `
	SELECT comment.id::text, comment.revision,
	       author.id::text, author.username, author.display_name, author.avatar_url,
	       comment.body, comment.created_at, comment.edited_at
	FROM revision_comments comment
	JOIN users author ON author.id = comment.author_id
`

func findRevisionComment(
	ctx context.Context,
	tx pgx.Tx,
	repositoryID string,
	revision string,
	commentID string,
	lock bool,
) (RevisionComment, error) {
	lockClause := ""
	if lock {
		lockClause = " FOR UPDATE OF comment"
	}
	comment, err := scanRevisionComment(tx.QueryRow(ctx, revisionCommentSelect+`
		WHERE comment.id = $1 AND comment.repository_id = $2 AND comment.revision = $3
	`+lockClause, commentID, repositoryID, revision))
	if errors.Is(err, pgx.ErrNoRows) {
		return RevisionComment{}, platform.ErrNotFound
	}
	if err != nil {
		return RevisionComment{}, fmt.Errorf("find revision comment: %w", err)
	}
	return comment, nil
}

func scanRevisionComments(rows pgx.Rows) ([]RevisionComment, error) {
	comments := make([]RevisionComment, 0)
	for rows.Next() {
		comment, err := scanRevisionComment(rows)
		if err != nil {
			return nil, fmt.Errorf("scan revision comment: %w", err)
		}
		comments = append(comments, comment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate revision comments: %w", err)
	}
	return comments, nil
}

type revisionCommentScanner interface {
	Scan(...any) error
}

func scanRevisionComment(row revisionCommentScanner) (RevisionComment, error) {
	var comment RevisionComment
	err := row.Scan(
		&comment.ID, &comment.Revision,
		&comment.Author.ID, &comment.Author.Username,
		&comment.Author.DisplayName, &comment.Author.AvatarURL,
		&comment.Body, &comment.CreatedAt, &comment.EditedAt,
	)
	return comment, err
}

func validateRevisionCommentRevision(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) != 64 || strings.Trim(value, "0123456789abcdef") != "" {
		return "", fmt.Errorf("revision comment revision is invalid: %w", platform.ErrInvalidInput)
	}
	return value, nil
}

func validateRevisionCommentBody(value string) (string, error) {
	if !validRevisionCommentBody(value) {
		return "", fmt.Errorf("revision comment body is invalid: %w", platform.ErrInvalidInput)
	}
	return value, nil
}

func validateRevisionCommentID(value string) (string, error) {
	parsed, err := uuid.Parse(value)
	if err != nil || parsed.String() != value {
		return "", fmt.Errorf("revision comment identifier is invalid: %w", platform.ErrInvalidInput)
	}
	return value, nil
}

func validRevisionCommentBody(value string) bool {
	if !utf8.ValidString(value) || len(value) > maxRevisionCommentBodyBytes || strings.TrimSpace(value) == "" {
		return false
	}
	for _, character := range value {
		if character == '\u007f' || character < '\u0020' && character != '\n' && character != '\r' && character != '\t' {
			return false
		}
	}
	return true
}

func normalizeRevisionCommentPage(page int, perPage int) (int, int, int, error) {
	if page < 1 || page > 1_000_000 {
		return 0, 0, 0, platform.ErrInvalidInput
	}
	if perPage == 0 {
		perPage = defaultRevisionCommentPageSize
	}
	if perPage < 1 || perPage > maxRevisionCommentPageSize {
		return 0, 0, 0, platform.ErrInvalidInput
	}
	maximum := int(^uint(0) >> 1)
	if page-1 > maximum/perPage {
		return 0, 0, 0, platform.ErrInvalidInput
	}
	return page, perPage, (page - 1) * perPage, nil
}
