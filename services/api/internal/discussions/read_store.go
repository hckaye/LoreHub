package discussions

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func (store *store) ListCategories(ctx context.Context, repositoryID string) ([]Category, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT category.id::text, category.slug, category.name, category.description,
		       category.format, COUNT(discussion.id), category.created_at, category.updated_at
		FROM discussion_categories category
		LEFT JOIN discussions discussion ON discussion.category_id = category.id
		WHERE category.repository_id = $1
		GROUP BY category.id
		ORDER BY lower(category.name), category.id
	`, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list discussion categories: %w", err)
	}
	defer rows.Close()
	categories := make([]Category, 0)
	for rows.Next() {
		var category Category
		if err := rows.Scan(
			&category.ID,
			&category.Slug,
			&category.Name,
			&category.Description,
			&category.Format,
			&category.DiscussionCount,
			&category.CreatedAt,
			&category.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan discussion category: %w", err)
		}
		categories = append(categories, category)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate discussion categories: %w", err)
	}
	return categories, nil
}

func (store *store) List(
	ctx context.Context,
	repositoryID string,
	viewerID string,
	filter ListFilter,
) (Page, error) {
	filter = normalizeListFilter(filter)
	searchPattern := discussionSearchPattern(filter.Query)
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return Page{}, fmt.Errorf("begin discussion list: %w", err)
	}
	defer rollback(ctx, tx)
	var total int64
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM discussions discussion
		JOIN discussion_categories category ON category.id = discussion.category_id
		WHERE discussion.repository_id = $1
		  AND ($2 = '' OR category.slug = $2)
		  AND ($3 = 'all' OR discussion.state = $3)
		  AND (
		    $4 = '' OR discussion.title ILIKE $4 ESCAPE '\'
		    OR discussion.body ILIKE $4 ESCAPE '\'
		  )
	`, repositoryID, filter.Category, filter.State, searchPattern).Scan(&total); err != nil {
		return Page{}, fmt.Errorf("count discussions: %w", err)
	}
	order := "discussion.pinned DESC, discussion.created_at DESC, discussion.id DESC"
	switch filter.Sort {
	case "oldest":
		order = "discussion.pinned DESC, discussion.created_at ASC, discussion.id ASC"
	case "most-commented":
		order = "discussion.pinned DESC, comment_count DESC, discussion.updated_at DESC, discussion.id DESC"
	case "most-voted":
		order = "discussion.pinned DESC, vote_count DESC, discussion.updated_at DESC, discussion.id DESC"
	}
	query := summarySelect + `
		WHERE discussion.repository_id = $1
		  AND ($3 = '' OR category.slug = $3)
		  AND ($4 = 'all' OR discussion.state = $4)
		  AND (
		    $5 = '' OR discussion.title ILIKE $5 ESCAPE '\'
		    OR discussion.body ILIKE $5 ESCAPE '\'
		  )
		ORDER BY ` + order + `
		LIMIT $6 OFFSET $7
	`
	rows, err := tx.Query(
		ctx,
		query,
		repositoryID,
		viewerID,
		filter.Category,
		filter.State,
		searchPattern,
		filter.PerPage,
		(filter.Page-1)*filter.PerPage,
	)
	if err != nil {
		return Page{}, fmt.Errorf("list discussions: %w", err)
	}
	defer rows.Close()
	discussions := make([]Summary, 0)
	for rows.Next() {
		discussion, err := scanSummary(rows)
		if err != nil {
			return Page{}, fmt.Errorf("scan discussion: %w", err)
		}
		discussions = append(discussions, discussion)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("iterate discussions: %w", err)
	}
	page := Page{
		Discussions: discussions,
		TotalCount:  total,
		Page:        filter.Page,
		PerPage:     filter.PerPage,
	}
	if err := commit(ctx, tx, "discussion list"); err != nil {
		return Page{}, err
	}
	return page, nil
}

func (store *store) Get(
	ctx context.Context,
	repositoryID string,
	number int64,
	viewerID string,
	commentPage int,
	commentsPerPage int,
) (Discussion, error) {
	commentPage, commentsPerPage = normalizePagination(commentPage, commentsPerPage)
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return Discussion{}, fmt.Errorf("begin discussion read: %w", err)
	}
	defer rollback(ctx, tx)
	summary, err := scanSummary(tx.QueryRow(ctx, summarySelect+`
		WHERE discussion.repository_id = $1 AND discussion.number = $3
	`, repositoryID, viewerID, number))
	if errors.Is(err, pgx.ErrNoRows) {
		return Discussion{}, platform.ErrNotFound
	}
	if err != nil {
		return Discussion{}, fmt.Errorf("get discussion: %w", err)
	}
	var body string
	var totalComments int64
	if err := tx.QueryRow(ctx, `
		SELECT discussion.body, COUNT(comment.id) FILTER (WHERE comment.archived_at IS NULL)
		FROM discussions discussion
		LEFT JOIN discussion_comments comment ON comment.discussion_id = discussion.id
		WHERE discussion.id = $1 AND discussion.repository_id = $2
		GROUP BY discussion.id
	`, summary.ID, repositoryID).Scan(&body, &totalComments); err != nil {
		return Discussion{}, fmt.Errorf("read discussion body: %w", err)
	}
	comments, err := store.listComments(
		ctx,
		tx,
		repositoryID,
		summary.ID,
		commentPage,
		commentsPerPage,
	)
	if err != nil {
		return Discussion{}, err
	}
	discussion := Discussion{
		Summary:         summary,
		Body:            body,
		Comments:        comments,
		CommentPage:     commentPage,
		CommentsPerPage: commentsPerPage,
		TotalComments:   totalComments,
	}
	if err := commit(ctx, tx, "discussion read"); err != nil {
		return Discussion{}, err
	}
	return discussion, nil
}

func (store *store) listComments(
	ctx context.Context,
	queries discussionQueries,
	repositoryID string,
	discussionID string,
	page int,
	perPage int,
) ([]Comment, error) {
	rows, err := queries.Query(ctx, `
		SELECT comment.id::text, comment.parent_id::text,
		       author.id::text, author.username, author.display_name, author.avatar_url,
		       comment.body, COALESCE(discussion.answered_comment_id = comment.id, false),
		       comment.created_at, comment.updated_at, comment.edited_at
		FROM discussion_comments comment
		JOIN discussions discussion
		  ON discussion.id = comment.discussion_id AND discussion.repository_id = comment.repository_id
		JOIN users author ON author.id = comment.author_id
		WHERE comment.repository_id = $1 AND comment.discussion_id = $2
		  AND comment.archived_at IS NULL
		ORDER BY comment.created_at, comment.id
		LIMIT $3 OFFSET $4
	`, repositoryID, discussionID, perPage, (page-1)*perPage)
	if err != nil {
		return nil, fmt.Errorf("list discussion comments: %w", err)
	}
	defer rows.Close()
	comments := make([]Comment, 0)
	for rows.Next() {
		var comment Comment
		if err := rows.Scan(
			&comment.ID,
			&comment.ParentID,
			&comment.Author.ID,
			&comment.Author.Username,
			&comment.Author.DisplayName,
			&comment.Author.AvatarURL,
			&comment.Body,
			&comment.Answer,
			&comment.CreatedAt,
			&comment.UpdatedAt,
			&comment.EditedAt,
		); err != nil {
			return nil, fmt.Errorf("scan discussion comment: %w", err)
		}
		comments = append(comments, comment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate discussion comments: %w", err)
	}
	return comments, nil
}

const summarySelect = `
	SELECT discussion.id::text, discussion.number, discussion.title, discussion.state,
	       discussion.locked, discussion.pinned, discussion.answered_comment_id IS NOT NULL,
	       category.id::text, category.slug, category.name, category.description, category.format,
	       0::bigint, category.created_at, category.updated_at,
	       author.id::text, author.username, author.display_name, author.avatar_url,
	       (
	         SELECT COUNT(*) FROM discussion_comments comment
	         WHERE comment.discussion_id = discussion.id AND comment.archived_at IS NULL
	       ) AS comment_count,
	       (SELECT COUNT(*) FROM discussion_votes vote WHERE vote.discussion_id = discussion.id) AS vote_count,
	       $2 <> '' AND EXISTS (
	         SELECT 1 FROM discussion_votes vote
	         WHERE vote.discussion_id = discussion.id AND vote.user_id = NULLIF($2, '')::uuid
	       ),
	       discussion.created_at, discussion.updated_at
	FROM discussions discussion
	JOIN discussion_categories category ON category.id = discussion.category_id
	JOIN users author ON author.id = discussion.author_id
`

func scanSummary(row rowScanner) (Summary, error) {
	var summary Summary
	err := row.Scan(
		&summary.ID,
		&summary.Number,
		&summary.Title,
		&summary.State,
		&summary.Locked,
		&summary.Pinned,
		&summary.Answered,
		&summary.Category.ID,
		&summary.Category.Slug,
		&summary.Category.Name,
		&summary.Category.Description,
		&summary.Category.Format,
		&summary.Category.DiscussionCount,
		&summary.Category.CreatedAt,
		&summary.Category.UpdatedAt,
		&summary.Author.ID,
		&summary.Author.Username,
		&summary.Author.DisplayName,
		&summary.Author.AvatarURL,
		&summary.CommentCount,
		&summary.VoteCount,
		&summary.ViewerHasVoted,
		&summary.CreatedAt,
		&summary.UpdatedAt,
	)
	return summary, err
}

func normalizeListFilter(filter ListFilter) ListFilter {
	filter.Category = normalizeCategorySlug(filter.Category)
	filter.Query = strings.TrimSpace(filter.Query)
	if filter.State != "open" && filter.State != "closed" {
		filter.State = "all"
	}
	if filter.Sort != "newest" && filter.Sort != "oldest" &&
		filter.Sort != "most-commented" && filter.Sort != "most-voted" {
		filter.Sort = "newest"
	}
	filter.Page, filter.PerPage = normalizePagination(filter.Page, filter.PerPage)
	return filter
}

func normalizePagination(page int, perPage int) (int, int) {
	if page < 1 || page > 10_000 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 50
	}
	return page, perPage
}

func discussionSearchPattern(query string) string {
	if query == "" {
		return ""
	}
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
	return "%" + escaped + "%"
}
