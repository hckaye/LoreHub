package collab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

// WorkItemEventStore reads the timeline events of one issue or pull request.
type WorkItemEventStore interface {
	ListWorkItemEvents(
		ctx context.Context, repositoryID, itemKind string, number int64, page Page,
	) (Result[WorkItemEvent], error)
}

// ListWorkItemEvents returns the timeline of an issue or pull request in
// chronological order. Callers must have already resolved a visible repository.
func (s *store) ListWorkItemEvents(
	ctx context.Context,
	repositoryID string,
	itemKind string,
	number int64,
	page Page,
) (Result[WorkItemEvent], error) {
	offset, err := pageOffset(page)
	if err != nil {
		return Result[WorkItemEvent]{}, err
	}
	limit := page.Limit
	if limit < 1 {
		limit = defaultPageLimit
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}
	itemID, err := s.workItemID(ctx, repositoryID, itemKind, number)
	if err != nil {
		return Result[WorkItemEvent]{}, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, item_kind, item_id, actor, event_kind, payload, created_at
		FROM work_item_events
		WHERE repository_id = $1 AND item_kind = $2 AND item_id = $3
		ORDER BY created_at ASC, id ASC
		LIMIT $4 OFFSET $5
	`, repositoryID, itemKind, itemID, limit+1, offset)
	if err != nil {
		return Result[WorkItemEvent]{}, fmt.Errorf("list work item events: %w", err)
	}
	defer rows.Close()
	events, err := scanWorkItemEvents(rows)
	if err != nil {
		return Result[WorkItemEvent]{}, err
	}
	return paginate(events, limit, offset), nil
}

func (s *store) workItemID(
	ctx context.Context,
	repositoryID string,
	itemKind string,
	number int64,
) (string, error) {
	query := `
		SELECT issue.id
		FROM issues issue
		JOIN repositories repository
		  ON repository.id = issue.repository_id AND repository.lifecycle_state = 'active'
		JOIN organizations organization
		  ON organization.id = repository.organization_id AND organization.active
		WHERE issue.repository_id = $1 AND issue.number = $2
	`
	if itemKind == WorkItemMergeRequest {
		query = `
			SELECT request.id
			FROM merge_requests request
			JOIN repositories repository
			  ON repository.id = request.repository_id AND repository.lifecycle_state = 'active'
			JOIN organizations organization
			  ON organization.id = repository.organization_id AND organization.active
			WHERE request.repository_id = $1 AND request.number = $2
		`
	}
	var itemID string
	err := s.pool.QueryRow(ctx, query, repositoryID, number).Scan(&itemID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", platform.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("find work item for events: %w", err)
	}
	return itemID, nil
}

func scanWorkItemEvents(rows pgx.Rows) ([]WorkItemEvent, error) {
	events := make([]WorkItemEvent, 0)
	for rows.Next() {
		var event WorkItemEvent
		var payload json.RawMessage
		if err := rows.Scan(
			&event.ID, &event.ItemKind, &event.ItemID, &event.Actor,
			&event.Kind, &payload, &event.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan work item event: %w", err)
		}
		if err := json.Unmarshal(payload, &event.Payload); err != nil {
			return nil, fmt.Errorf("decode work item event payload: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate work item events: %w", err)
	}
	return events, nil
}
