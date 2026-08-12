package webhooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type sourceEvent struct {
	ID        string
	Topic     string
	EventKey  string
	Payload   []byte
	CreatedAt time.Time
}

func resolveEventRepository(
	ctx context.Context,
	tx pgx.Tx,
	event sourceEvent,
) (repositoryRef, bool, error) {
	payload := make(map[string]json.RawMessage)
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return repositoryRef{}, false, nil
	}
	if repositoryID := payloadUUID(payload, "repositoryId"); repositoryID != "" {
		return loadEventRepository(ctx, tx, repositoryID)
	}
	entityID := strings.SplitN(event.EventKey, ":", 2)[0]
	if _, err := uuid.Parse(entityID); err != nil {
		return repositoryRef{}, false, nil
	}
	query, argument := eventRepositoryQuery(event.Topic, entityID, payload)
	if query != "" && argument != "" {
		var repositoryID string
		err := tx.QueryRow(ctx, query, argument).Scan(&repositoryID)
		if err == nil {
			return loadEventRepository(ctx, tx, repositoryID)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return repositoryRef{}, false, fmt.Errorf("resolve webhook event repository: %w", err)
		}
	}
	targetType := auditTargetType(event.Topic)
	if targetType == "" {
		return repositoryRef{}, false, nil
	}
	var repositoryID string
	err := tx.QueryRow(ctx, `
		SELECT repository_id
		FROM audit_events
		WHERE target_type = $1 AND target_id = $2 AND repository_id IS NOT NULL
		ORDER BY occurred_at DESC, id DESC
		LIMIT 1
	`, targetType, entityID).Scan(&repositoryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return repositoryRef{}, false, nil
	}
	if err != nil {
		return repositoryRef{}, false, fmt.Errorf("resolve deleted webhook event repository: %w", err)
	}
	return loadEventRepository(ctx, tx, repositoryID)
}

func eventRepositoryQuery(
	topic string,
	entityID string,
	payload map[string]json.RawMessage,
) (string, string) {
	switch {
	case strings.HasPrefix(topic, "repository."):
		return `SELECT id FROM repositories WHERE id = $1`, entityID
	case strings.HasPrefix(topic, "issue_comment."):
		if issueID := payloadUUID(payload, "issueId"); issueID != "" {
			return `SELECT repository_id FROM issues WHERE id = $1`, issueID
		}
		return `
			SELECT issue.repository_id FROM issue_comments comment
			JOIN issues issue ON issue.id = comment.issue_id WHERE comment.id = $1
		`, entityID
	case strings.HasPrefix(topic, "issue_label."), strings.HasPrefix(topic, "issue."):
		if issueID := payloadUUID(payload, "issueId"); issueID != "" {
			entityID = issueID
		}
		return `SELECT repository_id FROM issues WHERE id = $1`, entityID
	case strings.HasPrefix(topic, "merge_request_comment."):
		if requestID := payloadUUID(payload, "mergeRequestId"); requestID != "" {
			return `SELECT repository_id FROM merge_requests WHERE id = $1`, requestID
		}
		return `
			SELECT request.repository_id FROM merge_request_comments comment
			JOIN merge_requests request ON request.id = comment.merge_request_id WHERE comment.id = $1
		`, entityID
	case strings.HasPrefix(topic, "merge_request_review_request."):
		return `SELECT repository_id FROM merge_request_review_requests WHERE id = $1`, entityID
	case strings.HasPrefix(topic, "merge_request_review."):
		return `
			SELECT request.repository_id FROM merge_request_reviews review
			JOIN merge_requests request ON request.id = review.merge_request_id WHERE review.id = $1
		`, entityID
	case strings.HasPrefix(topic, "merge_request_review_thread."),
		strings.HasPrefix(topic, "merge_request_review_comment."):
		return `SELECT repository_id FROM merge_request_review_threads WHERE id = $1`, entityID
	case strings.HasPrefix(topic, "merge_request."):
		return `SELECT repository_id FROM merge_requests WHERE id = $1`, entityID
	case strings.HasPrefix(topic, "merge_operation."):
		return `SELECT repository_id FROM merge_operations WHERE id = $1`, entityID
	case strings.HasPrefix(topic, "label."):
		return `SELECT repository_id FROM labels WHERE id = $1`, entityID
	case strings.HasPrefix(topic, "branch_rule."):
		return `SELECT repository_id FROM branch_rules WHERE id = $1`, entityID
	case strings.HasPrefix(topic, "milestone."):
		return `SELECT repository_id FROM repository_milestones WHERE id = $1`, entityID
	case strings.HasPrefix(topic, "project."):
		return `SELECT repository_id FROM projects WHERE id = $1`, entityID
	case strings.HasPrefix(topic, "release.asset."):
		return `SELECT repository_id FROM release_asset_links WHERE id = $1`, entityID
	case strings.HasPrefix(topic, "release."):
		return `SELECT repository_id FROM repository_releases WHERE id = $1`, entityID
	case strings.HasPrefix(topic, "wiki."):
		return `SELECT repository_id FROM repository_wiki_pages WHERE id = $1`, entityID
	default:
		return "", ""
	}
}

func auditTargetType(topic string) string {
	switch {
	case strings.HasPrefix(topic, "repository."):
		return "repository"
	case strings.HasPrefix(topic, "issue_comment."):
		return "issue_comment"
	case strings.HasPrefix(topic, "issue_label."), strings.HasPrefix(topic, "issue."):
		return "issue"
	case strings.HasPrefix(topic, "merge_request_comment."):
		return "merge_request_comment"
	case strings.HasPrefix(topic, "merge_request_review_request."):
		return "merge_request_review_request"
	case strings.HasPrefix(topic, "merge_request_review."):
		return "merge_request_review"
	case strings.HasPrefix(topic, "merge_request_review_thread."):
		return "merge_request_review_thread"
	case strings.HasPrefix(topic, "merge_request_review_comment."):
		return "merge_request_review_comment"
	case strings.HasPrefix(topic, "merge_request."):
		return "merge_request"
	case strings.HasPrefix(topic, "merge_operation."):
		return "merge_operation"
	case strings.HasPrefix(topic, "label."):
		return "label"
	case strings.HasPrefix(topic, "branch_rule."):
		return "branch_rule"
	case strings.HasPrefix(topic, "milestone."):
		return "milestone"
	case strings.HasPrefix(topic, "project."):
		return "project"
	case strings.HasPrefix(topic, "release.asset."):
		return "release_asset"
	case strings.HasPrefix(topic, "release."):
		return "release"
	case strings.HasPrefix(topic, "actions."):
		return "ci_run"
	case strings.HasPrefix(topic, "branch."):
		return "lore_branch"
	case strings.HasPrefix(topic, "wiki."):
		return "wiki_page"
	default:
		return ""
	}
}

func payloadUUID(payload map[string]json.RawMessage, name string) string {
	encoded, found := payload[name]
	if !found {
		return ""
	}
	var value string
	if err := json.Unmarshal(encoded, &value); err != nil {
		return ""
	}
	if _, err := uuid.Parse(value); err != nil {
		return ""
	}
	return value
}

func loadEventRepository(
	ctx context.Context,
	tx pgx.Tx,
	repositoryID string,
) (repositoryRef, bool, error) {
	var reference repositoryRef
	err := tx.QueryRow(ctx, `
		SELECT repository.id, organization.id, organization.slug, repository.slug
		FROM repositories repository
		JOIN organizations organization
		  ON organization.id = repository.organization_id AND organization.active
		WHERE repository.id = $1 AND repository.lifecycle_state = 'active'
	`, repositoryID).Scan(
		&reference.ID, &reference.OrganizationID, &reference.Owner, &reference.Slug,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return repositoryRef{}, false, nil
	}
	if err != nil {
		return repositoryRef{}, false, fmt.Errorf("load webhook event repository: %w", err)
	}
	return reference, true, nil
}
