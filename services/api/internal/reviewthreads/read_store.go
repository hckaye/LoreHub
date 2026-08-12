package reviewthreads

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func (store *store) List(ctx context.Context, repositoryID string, number int64) ([]Thread, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT thread.id, thread.path, thread.side, thread.line_number, thread.line_content,
		       thread.base_revision, thread.head_revision,
		       thread.base_revision <> request.target_revision
		         OR thread.head_revision <> request.source_revision,
		       thread.resolved, thread.version, creator.username, thread.created_by,
		       resolver.username, thread.created_at, thread.updated_at, thread.resolved_at,
		       request.author_id
		FROM merge_request_review_threads thread
		JOIN merge_requests request ON request.id = thread.merge_request_id
		  AND request.repository_id = thread.repository_id
		JOIN repositories repository ON repository.id = request.repository_id
		  AND repository.lifecycle_state = 'active'
		JOIN organizations organization ON organization.id = repository.organization_id AND organization.active
		JOIN users creator ON creator.id = thread.created_by
		LEFT JOIN users resolver ON resolver.id = thread.resolved_by
		WHERE repository.id = $1 AND request.number = $2
		ORDER BY thread.path, thread.line_number, thread.created_at, thread.id
	`, repositoryID, number)
	if err != nil {
		return nil, fmt.Errorf("list review threads: %w", err)
	}
	defer rows.Close()
	threads := make([]Thread, 0)
	byID := make(map[string]int)
	for rows.Next() {
		var thread Thread
		if err := rows.Scan(
			&thread.ID, &thread.Path, &thread.Side, &thread.LineNumber, &thread.LineContent,
			&thread.BaseRevision, &thread.HeadRevision, &thread.Outdated,
			&thread.Resolved, &thread.Version, &thread.CreatedBy, &thread.createdByID,
			&thread.ResolvedBy, &thread.CreatedAt, &thread.UpdatedAt, &thread.ResolvedAt,
			&thread.mergeAuthorID,
		); err != nil {
			return nil, fmt.Errorf("scan review thread: %w", err)
		}
		thread.Comments = make([]Comment, 0)
		byID[thread.ID] = len(threads)
		threads = append(threads, thread)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate review threads: %w", err)
	}
	if len(threads) == 0 {
		var exists bool
		if err := store.pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM merge_requests WHERE repository_id = $1 AND number = $2
			)
		`, repositoryID, number).Scan(&exists); err != nil {
			return nil, fmt.Errorf("check pull request for review threads: %w", err)
		}
		if !exists {
			return nil, platform.ErrNotFound
		}
		return threads, nil
	}
	commentRows, err := store.pool.Query(ctx, `
		SELECT comment.thread_id, comment.id, author.username, comment.author_id,
		       comment.body, comment.deleted_at IS NOT NULL, comment.version,
		       comment.created_at, comment.updated_at, comment.edited_at
		FROM merge_request_review_comments comment
		JOIN users author ON author.id = comment.author_id
		JOIN merge_request_review_threads thread ON thread.id = comment.thread_id
		WHERE thread.merge_request_id = (
			SELECT id FROM merge_requests WHERE repository_id = $1 AND number = $2
		)
		ORDER BY comment.created_at, comment.id
	`, repositoryID, number)
	if err != nil {
		return nil, fmt.Errorf("list review thread comments: %w", err)
	}
	defer commentRows.Close()
	for commentRows.Next() {
		var threadID string
		var comment Comment
		if err := commentRows.Scan(
			&threadID, &comment.ID, &comment.Author, &comment.authorID,
			&comment.Body, &comment.Deleted, &comment.Version,
			&comment.CreatedAt, &comment.UpdatedAt, &comment.EditedAt,
		); err != nil {
			return nil, fmt.Errorf("scan review thread comment: %w", err)
		}
		if index, ok := byID[threadID]; ok {
			threads[index].Comments = append(threads[index].Comments, comment)
		}
	}
	if err := commentRows.Err(); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("iterate review thread comments: %w", err)
	}
	return threads, nil
}
