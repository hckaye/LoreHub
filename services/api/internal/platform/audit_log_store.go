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
)

const (
	defaultAuditLogLimit = 50
	maxAuditLogLimit     = 100
)

var ErrInvalidAuditCursor = errors.New("audit log cursor is invalid")

type auditCursor struct {
	OccurredAt time.Time
	ID         uuid.UUID
}

func (store *Store) OrganizationAuditLog(
	ctx context.Context,
	actor User,
	organizationSlug string,
	query string,
	cursor string,
	limit int,
) (AuditLogPage, error) {
	organizationID, role, err := store.organizationRole(ctx, actor.ID, organizationSlug)
	if err != nil {
		return AuditLogPage{}, err
	}
	if role != "owner" {
		return AuditLogPage{}, ErrForbidden
	}
	boundary, err := decodeAuditCursor(cursor)
	if err != nil {
		return AuditLogPage{}, err
	}
	if limit < 1 || limit > maxAuditLogLimit {
		limit = defaultAuditLogLimit
	}
	query = strings.TrimSpace(query)
	rows, err := store.pool.Query(ctx, `
		SELECT event.id, event.action, event.target_type, event.target_id,
		       actor.id, COALESCE(event.actor_username, actor.username),
		       COALESCE(event.actor_display_name, actor.display_name),
		       repository.id, COALESCE(event.repository_owner, organization.slug),
		       COALESCE(event.repository_slug, repository.slug), host(event.remote_address),
		       event.details, event.occurred_at
		FROM audit_events event
		LEFT JOIN users actor ON actor.id = event.actor_id
		LEFT JOIN repositories repository ON repository.id = event.repository_id
		LEFT JOIN organizations organization ON organization.id = repository.organization_id
		WHERE event.organization_id = $1
		  AND (
		      $2 = ''
		      OR strpos(lower(concat_ws(' ', event.action, event.target_type, event.target_id,
		          event.actor_username, actor.username, event.actor_display_name, actor.display_name,
		          event.repository_owner, event.repository_slug, repository.slug)), lower($2)) > 0
		  )
		  AND (
		      $3::timestamptz IS NULL
		      OR (event.occurred_at, event.id) < ($3::timestamptz, $4::uuid)
		  )
		ORDER BY event.occurred_at DESC, event.id DESC
		LIMIT $5
	`, organizationID, query, cursorTime(boundary), cursorID(boundary), limit+1)
	if err != nil {
		return AuditLogPage{}, fmt.Errorf("list organization audit events: %w", err)
	}
	defer rows.Close()
	items := make([]AuditEvent, 0, limit+1)
	for rows.Next() {
		event, err := scanAuditEvent(rows, organizationSlug)
		if err != nil {
			return AuditLogPage{}, err
		}
		items = append(items, event)
	}
	if err := rows.Err(); err != nil {
		return AuditLogPage{}, fmt.Errorf("iterate organization audit events: %w", err)
	}
	page := AuditLogPage{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		next := encodeAuditCursor(page.Items[len(page.Items)-1])
		page.NextCursor = &next
	}
	return page, nil
}

func scanAuditEvent(row interface{ Scan(...any) error }, organizationSlug string) (AuditEvent, error) {
	var event AuditEvent
	var actorID, actorUsername, actorDisplayName *string
	var repositoryID, repositoryOwner, repositorySlug *string
	var details []byte
	if err := row.Scan(
		&event.ID, &event.Action, &event.TargetType, &event.TargetID,
		&actorID, &actorUsername, &actorDisplayName,
		&repositoryID, &repositoryOwner, &repositorySlug, &event.RemoteAddress,
		&details, &event.OccurredAt,
	); err != nil {
		return AuditEvent{}, fmt.Errorf("scan organization audit event: %w", err)
	}
	if actorID != nil || actorUsername != nil {
		event.Actor = &AuditActor{
			ID: valueOrBlank(actorID), Username: valueOrBlank(actorUsername), DisplayName: valueOrBlank(actorDisplayName),
		}
	}
	if repositoryID != nil || repositorySlug != nil {
		event.Repository = &AuditRepository{
			ID: valueOrBlank(repositoryID), Owner: organizationSlug, Slug: valueOrBlank(repositorySlug),
		}
		if repositoryOwner != nil {
			event.Repository.Owner = *repositoryOwner
		}
	}
	if err := json.Unmarshal(details, &event.Details); err != nil {
		return AuditEvent{}, fmt.Errorf("decode organization audit event details: %w", err)
	}
	if event.Details == nil {
		event.Details = map[string]any{}
	}
	event.Details = redactAuditDetails(event.Details)
	return event, nil
}

func redactAuditDetails(details map[string]any) map[string]any {
	redacted := make(map[string]any, len(details))
	for key, value := range details {
		if sensitiveAuditDetailKey(key) {
			redacted[key] = "[REDACTED]"
			continue
		}
		redacted[key] = redactAuditDetailValue(value)
	}
	return redacted
}

func redactAuditDetailValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return redactAuditDetails(typed)
	case []any:
		items := make([]any, len(typed))
		for index, item := range typed {
			items[index] = redactAuditDetailValue(item)
		}
		return items
	default:
		return value
	}
}

func sensitiveAuditDetailKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "authorization", "ciphertext", "credential", "encrypted_value", "nonce", "password", "secret",
		"token", "value":
		return true
	default:
		return false
	}
}

func valueOrBlank(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func encodeAuditCursor(event AuditEvent) string {
	value := event.OccurredAt.UTC().Format(time.RFC3339Nano) + "|" + event.ID
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func decodeAuditCursor(value string) (*auditCursor, error) {
	if value == "" {
		return nil, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, ErrInvalidAuditCursor
	}
	parts := strings.Split(string(decoded), "|")
	if len(parts) != 2 {
		return nil, ErrInvalidAuditCursor
	}
	occurredAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return nil, ErrInvalidAuditCursor
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return nil, ErrInvalidAuditCursor
	}
	return &auditCursor{OccurredAt: occurredAt.UTC(), ID: id}, nil
}

func cursorTime(cursor *auditCursor) any {
	if cursor == nil {
		return nil
	}
	return cursor.OccurredAt
}

func cursorID(cursor *auditCursor) any {
	if cursor == nil {
		return nil
	}
	return cursor.ID
}
