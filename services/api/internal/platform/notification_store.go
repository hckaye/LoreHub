package platform

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

type notificationEvent struct {
	ID        string
	Topic     string
	EventKey  string
	Payload   []byte
	CreatedAt time.Time
}

type notificationScope struct {
	OrganizationID     string
	RepositoryID       string
	TeamID             string
	OrganizationSlug   string
	RepositorySlug     string
	Visibility         string
	IssueNumber        *int64
	MergeRequestNumber *int64
	Title              string
	Href               string
}

func (store *Store) ListNotifications(
	ctx context.Context,
	actor User,
	unreadOnly bool,
	limit int,
) (NotificationPage, error) {
	if limit < 1 || limit > 100 {
		limit = 30
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return NotificationPage{}, fmt.Errorf("begin notification projection: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	if err := store.syncNotifications(ctx, transaction); err != nil {
		return NotificationPage{}, err
	}
	var total int64
	if err := transaction.QueryRow(ctx, `
		SELECT COUNT(*) FROM notifications
		WHERE recipient_id = $1 AND ($2 = false OR read_at IS NULL)
	`, actor.ID, unreadOnly).Scan(&total); err != nil {
		return NotificationPage{}, fmt.Errorf("count notifications: %w", err)
	}
	rows, err := transaction.Query(ctx, `
		SELECT id, topic, title, body, href, read_at, created_at
		FROM notifications
		WHERE recipient_id = $1 AND ($2 = false OR read_at IS NULL)
		ORDER BY created_at DESC, id DESC
		LIMIT $3
	`, actor.ID, unreadOnly, limit)
	if err != nil {
		return NotificationPage{}, fmt.Errorf("list notifications: %w", err)
	}
	items, err := scanNotifications(rows)
	rows.Close()
	if err != nil {
		return NotificationPage{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return NotificationPage{}, fmt.Errorf("commit notification projection: %w", err)
	}
	return NotificationPage{Items: items, Total: total}, nil
}

func (store *Store) UnreadNotificationCount(ctx context.Context, actor User) (int64, error) {
	page, err := store.ListNotifications(ctx, actor, true, 1)
	if err != nil {
		return 0, err
	}
	return page.Total, nil
}

func (store *Store) MarkNotificationRead(ctx context.Context, actor User, notificationID string) error {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin notification read update: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	tag, err := transaction.Exec(ctx, `
		UPDATE notifications SET read_at = COALESCE(read_at, now())
		WHERE id = $1 AND recipient_id = $2
	`, notificationID, actor.ID)
	if err != nil {
		return fmt.Errorf("mark notification read: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := insertAudit(
		ctx, transaction, actor.ID, "", "", "notification.read", "notification", notificationID,
	); err != nil {
		return err
	}
	if err := insertOutbox(ctx, transaction, "notification.read", notificationID+":"+uuid.NewString(), map[string]string{
		"notificationId": notificationID,
	}); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit notification read update: %w", err)
	}
	return nil
}

func (store *Store) MarkAllNotificationsRead(ctx context.Context, actor User) error {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin all notifications read update: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := transaction.Exec(ctx, `
		UPDATE notifications SET read_at = now() WHERE recipient_id = $1 AND read_at IS NULL
	`, actor.ID); err != nil {
		return fmt.Errorf("mark all notifications read: %w", err)
	}
	if err := insertAudit(ctx, transaction, actor.ID, "", "", "notification.read_all", "user", actor.ID); err != nil {
		return err
	}
	if err := insertOutbox(ctx, transaction, "notification.read_all", actor.ID+":"+uuid.NewString(), map[string]string{
		"userId": actor.ID,
	}); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit all notifications read update: %w", err)
	}
	return nil
}

func (store *Store) NotificationPreferences(
	ctx context.Context,
	actor User,
) (NotificationPreferences, error) {
	if _, err := store.pool.Exec(ctx, `
		INSERT INTO notification_preferences (user_id) VALUES ($1) ON CONFLICT (user_id) DO NOTHING
	`, actor.ID); err != nil {
		return NotificationPreferences{}, fmt.Errorf("create notification preferences: %w", err)
	}
	return store.readNotificationPreferences(ctx, actor.ID)
}

func (store *Store) UpdateNotificationPreferences(
	ctx context.Context,
	actor User,
	input UpdateNotificationPreferencesInput,
) (NotificationPreferences, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return NotificationPreferences{}, fmt.Errorf("begin notification preferences update: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	_, err = transaction.Exec(ctx, `
		INSERT INTO notification_preferences (user_id, in_app_enabled, email_enabled, mention_enabled,
		    team_enabled, repository_enabled, updated_at)
		VALUES ($1, COALESCE($2, true), COALESCE($3, false), COALESCE($4, true),
		    COALESCE($5, true), COALESCE($6, true), now())
		ON CONFLICT (user_id) DO UPDATE SET
		    in_app_enabled = COALESCE($2, notification_preferences.in_app_enabled),
		    email_enabled = COALESCE($3, notification_preferences.email_enabled),
		    mention_enabled = COALESCE($4, notification_preferences.mention_enabled),
		    team_enabled = COALESCE($5, notification_preferences.team_enabled),
		    repository_enabled = COALESCE($6, notification_preferences.repository_enabled),
		    updated_at = now()
	`, actor.ID, input.InAppEnabled, input.EmailEnabled, input.MentionEnabled, input.TeamEnabled,
		input.RepositoryEnabled)
	if err != nil {
		return NotificationPreferences{}, fmt.Errorf("update notification preferences: %w", err)
	}
	if err := insertAudit(
		ctx, transaction, actor.ID, "", "", "notification_preferences.update", "user", actor.ID,
	); err != nil {
		return NotificationPreferences{}, err
	}
	if err := insertOutbox(
		ctx, transaction, "notification_preferences.updated", actor.ID+":"+uuid.NewString(), input,
	); err != nil {
		return NotificationPreferences{}, err
	}
	var preferences NotificationPreferences
	err = transaction.QueryRow(ctx, `
		SELECT in_app_enabled, email_enabled, mention_enabled, team_enabled, repository_enabled, updated_at
		FROM notification_preferences WHERE user_id = $1
	`, actor.ID).Scan(
		&preferences.InAppEnabled,
		&preferences.EmailEnabled,
		&preferences.MentionEnabled,
		&preferences.TeamEnabled,
		&preferences.RepositoryEnabled,
		&preferences.UpdatedAt,
	)
	if err != nil {
		return NotificationPreferences{}, fmt.Errorf("read updated notification preferences: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return NotificationPreferences{}, fmt.Errorf("commit notification preferences update: %w", err)
	}
	return preferences, nil
}

func (store *Store) readNotificationPreferences(ctx context.Context, userID string) (NotificationPreferences, error) {
	var preferences NotificationPreferences
	err := store.pool.QueryRow(ctx, `
		SELECT in_app_enabled, email_enabled, mention_enabled, team_enabled, repository_enabled, updated_at
		FROM notification_preferences WHERE user_id = $1
	`, userID).Scan(
		&preferences.InAppEnabled,
		&preferences.EmailEnabled,
		&preferences.MentionEnabled,
		&preferences.TeamEnabled,
		&preferences.RepositoryEnabled,
		&preferences.UpdatedAt,
	)
	if err != nil {
		return NotificationPreferences{}, fmt.Errorf("read notification preferences: %w", err)
	}
	return preferences, nil
}

const notificationProjectionBatchSize = 100

func (store *Store) syncNotifications(ctx context.Context, transaction pgx.Tx) error {
	rows, err := transaction.Query(ctx, `
		WITH candidates AS MATERIALIZED (
			SELECT events.id, events.topic, events.event_key, events.payload, events.created_at
			FROM outbox_events events
			LEFT JOIN notification_projection_ledger ledger
			  ON ledger.source_event_id = events.id
			WHERE events.topic IN (
			    'organization.created', 'organization.updated', 'repository.registered',
			    'repository.settings_updated', 'issue.created', 'issue.updated',
			    'issue_comment.created', 'issue_comment.updated', 'issue_comment.deleted',
			    'merge_request.created', 'merge_request.updated',
			    'merge_request_review.created', 'merge_request_review.updated',
			    'label.created', 'label.updated', 'label.deleted', 'branch_rule.created',
			    'branch_rule.updated', 'branch_rule.deleted', 'team.created', 'team.updated',
			    'team.member_added', 'team.member_removed'
			)
			AND (
				ledger.source_event_id IS NULL
				OR (
					ledger.status = 'processing'
					AND ledger.claimed_at < now() - interval '5 minutes'
				)
			)
			ORDER BY events.created_at ASC, events.id ASC
			LIMIT `+fmt.Sprint(notificationProjectionBatchSize)+`
			FOR UPDATE OF events SKIP LOCKED
		), claimed AS (
			INSERT INTO notification_projection_ledger (source_event_id, status, claimed_at, processed_at)
			SELECT id, 'processing', now(), NULL FROM candidates
			ON CONFLICT (source_event_id) DO UPDATE SET
				status = 'processing', claimed_at = EXCLUDED.claimed_at, processed_at = NULL
			RETURNING source_event_id
		)
		SELECT candidates.id, candidates.topic, candidates.event_key, candidates.payload, candidates.created_at
		FROM candidates
		JOIN claimed ON claimed.source_event_id = candidates.id
		ORDER BY candidates.created_at ASC, candidates.id ASC
	`)
	if err != nil {
		return fmt.Errorf("claim notification source events: %w", err)
	}
	events := make([]notificationEvent, 0)
	for rows.Next() {
		var event notificationEvent
		if err := rows.Scan(&event.ID, &event.Topic, &event.EventKey, &event.Payload, &event.CreatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan notification source event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate claimed notification source events: %w", err)
	}
	rows.Close()
	for _, event := range events {
		scope, found, err := store.resolveNotificationScope(ctx, transaction, event)
		if err != nil {
			return err
		}
		if found {
			preferences, err := store.notificationRecipients(ctx, transaction, scope, event.Topic)
			if err != nil {
				return err
			}
			message := notificationMessage(event, scope)
			for _, recipient := range preferences {
				_, err := transaction.Exec(ctx, `
					INSERT INTO notifications (
					    id, recipient_id, source_event_id, topic, title, body, href, created_at
					) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
					ON CONFLICT (recipient_id, source_event_id) DO NOTHING
				`, uuid.NewString(), recipient, event.ID, event.Topic, message.title, message.body, message.href,
					event.CreatedAt)
				if err != nil {
					return fmt.Errorf("materialize notification: %w", err)
				}
			}
		}
		if _, err := transaction.Exec(ctx, `
			UPDATE notification_projection_ledger
			SET status = 'processed', processed_at = now()
			WHERE source_event_id = $1 AND status = 'processing'
		`, event.ID); err != nil {
			return fmt.Errorf("mark notification source event processed: %w", err)
		}
	}
	return nil
}

func (store *Store) resolveNotificationScope(
	ctx context.Context,
	transaction pgx.Tx,
	event notificationEvent,
) (notificationScope, bool, error) {
	var scope notificationScope
	var issueNumber, mergeRequestNumber *int64
	scope.IssueNumber = issueNumber
	scope.MergeRequestNumber = mergeRequestNumber
	eventID, valid := notificationEventID(event)
	if !valid {
		return notificationScope{}, false, nil
	}
	var err error
	switch {
	case strings.HasPrefix(event.Topic, "organization."):
		err = transaction.QueryRow(ctx, `
			SELECT id, '', slug, '', NULL, NULL, display_name
			FROM organizations WHERE id = split_part($1, ':', 1)::uuid
		`, eventID).Scan(&scope.OrganizationID, &scope.RepositoryID, &scope.OrganizationSlug,
			&scope.RepositorySlug, &scope.IssueNumber, &scope.MergeRequestNumber, &scope.Title)
	case strings.HasPrefix(event.Topic, "repository."):
		err = transaction.QueryRow(ctx, `
			SELECT r.organization_id, r.id, o.slug, r.slug, r.visibility, NULL, NULL, r.display_name
			FROM repositories r JOIN organizations o ON o.id = r.organization_id
			WHERE r.id = split_part($1, ':', 1)::uuid
		`, eventID).Scan(&scope.OrganizationID, &scope.RepositoryID, &scope.OrganizationSlug,
			&scope.RepositorySlug, &scope.Visibility, &scope.IssueNumber, &scope.MergeRequestNumber,
			&scope.Title)
	case strings.HasPrefix(event.Topic, "issue_comment."):
		err = transaction.QueryRow(ctx, `
			SELECT r.organization_id, r.id, o.slug, r.slug, r.visibility, i.number, NULL, i.title
			FROM issue_comments c
			JOIN issues i ON i.id = c.issue_id
			JOIN repositories r ON r.id = i.repository_id
			JOIN organizations o ON o.id = r.organization_id
			WHERE c.id = split_part($1, ':', 1)::uuid
		`, eventID).Scan(&scope.OrganizationID, &scope.RepositoryID, &scope.OrganizationSlug,
			&scope.RepositorySlug, &scope.Visibility, &scope.IssueNumber, &scope.MergeRequestNumber,
			&scope.Title)
	case strings.HasPrefix(event.Topic, "issue."):
		err = transaction.QueryRow(ctx, `
			SELECT r.organization_id, r.id, o.slug, r.slug, r.visibility, i.number, NULL, i.title
			FROM issues i JOIN repositories r ON r.id = i.repository_id
			JOIN organizations o ON o.id = r.organization_id
			WHERE i.id = split_part($1, ':', 1)::uuid
		`, eventID).Scan(&scope.OrganizationID, &scope.RepositoryID, &scope.OrganizationSlug,
			&scope.RepositorySlug, &scope.Visibility, &scope.IssueNumber, &scope.MergeRequestNumber,
			&scope.Title)
	case strings.HasPrefix(event.Topic, "merge_request_review."):
		err = transaction.QueryRow(ctx, `
			SELECT r.organization_id, r.id, o.slug, r.slug, r.visibility, NULL, mr.number, mr.title
			FROM merge_request_reviews review
			JOIN merge_requests mr ON mr.id = review.merge_request_id
			JOIN repositories r ON r.id = mr.repository_id
			JOIN organizations o ON o.id = r.organization_id
			WHERE review.id = split_part($1, ':', 1)::uuid
		`, eventID).Scan(&scope.OrganizationID, &scope.RepositoryID, &scope.OrganizationSlug,
			&scope.RepositorySlug, &scope.Visibility, &scope.IssueNumber, &scope.MergeRequestNumber,
			&scope.Title)
	case strings.HasPrefix(event.Topic, "merge_request."):
		err = transaction.QueryRow(ctx, `
			SELECT r.organization_id, r.id, o.slug, r.slug, r.visibility, NULL, mr.number, mr.title
			FROM merge_requests mr JOIN repositories r ON r.id = mr.repository_id
			JOIN organizations o ON o.id = r.organization_id
			WHERE mr.id = split_part($1, ':', 1)::uuid
		`, eventID).Scan(&scope.OrganizationID, &scope.RepositoryID, &scope.OrganizationSlug,
			&scope.RepositorySlug, &scope.Visibility, &scope.IssueNumber, &scope.MergeRequestNumber,
			&scope.Title)
	case strings.HasPrefix(event.Topic, "label."):
		err = transaction.QueryRow(ctx, `
			SELECT r.organization_id, r.id, o.slug, r.slug, r.visibility, NULL, NULL, l.name
			FROM labels l JOIN repositories r ON r.id = l.repository_id
			JOIN organizations o ON o.id = r.organization_id
			WHERE l.id = split_part($1, ':', 1)::uuid
		`, eventID).Scan(&scope.OrganizationID, &scope.RepositoryID, &scope.OrganizationSlug,
			&scope.RepositorySlug, &scope.Visibility, &scope.IssueNumber, &scope.MergeRequestNumber,
			&scope.Title)
	case strings.HasPrefix(event.Topic, "branch_rule."):
		err = transaction.QueryRow(ctx, `
			SELECT r.organization_id, r.id, o.slug, r.slug, r.visibility, NULL, NULL, br.pattern
			FROM branch_rules br JOIN repositories r ON r.id = br.repository_id
			JOIN organizations o ON o.id = r.organization_id
			WHERE br.id = split_part($1, ':', 1)::uuid
		`, eventID).Scan(&scope.OrganizationID, &scope.RepositoryID, &scope.OrganizationSlug,
			&scope.RepositorySlug, &scope.Visibility, &scope.IssueNumber, &scope.MergeRequestNumber,
			&scope.Title)
	case strings.HasPrefix(event.Topic, "team."):
		err = transaction.QueryRow(ctx, `
			SELECT o.id, '', o.slug, '', NULL, NULL, t.display_name
			FROM teams t JOIN organizations o ON o.id = t.organization_id
			WHERE t.id = split_part($1, ':', 1)::uuid
		`, eventID).Scan(&scope.OrganizationID, &scope.RepositoryID, &scope.OrganizationSlug,
			&scope.RepositorySlug, &scope.IssueNumber, &scope.MergeRequestNumber, &scope.Title)
	default:
		return notificationScope{}, false, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return store.resolveDeletedNotificationScope(ctx, transaction, event)
	}
	if err != nil {
		return notificationScope{}, false, fmt.Errorf("resolve notification scope: %w", err)
	}
	if strings.HasPrefix(event.Topic, "team.") {
		scope.TeamID = eventID
	}
	scope.Href = notificationHref(scope)
	return scope, true, nil
}

func (store *Store) resolveDeletedNotificationScope(
	ctx context.Context,
	transaction pgx.Tx,
	event notificationEvent,
) (notificationScope, bool, error) {
	var payload struct {
		IssueID      string `json:"issueId"`
		RepositoryID string `json:"repositoryId"`
		Name         string `json:"name"`
		Pattern      string `json:"pattern"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return notificationScope{}, false, nil
	}
	var scope notificationScope
	switch {
	case strings.HasPrefix(event.Topic, "issue_comment.") && payload.IssueID != "":
		if _, err := uuid.Parse(payload.IssueID); err != nil {
			return notificationScope{}, false, nil
		}
		err := transaction.QueryRow(ctx, `
			SELECT r.organization_id, r.id, o.slug, r.slug, r.visibility, i.number, NULL, i.title
			FROM issues i JOIN repositories r ON r.id = i.repository_id
			JOIN organizations o ON o.id = r.organization_id
			WHERE i.id = $1
		`, payload.IssueID).Scan(&scope.OrganizationID, &scope.RepositoryID, &scope.OrganizationSlug,
			&scope.RepositorySlug, &scope.Visibility, &scope.IssueNumber, &scope.MergeRequestNumber,
			&scope.Title)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return notificationScope{}, false, nil
			}
			return notificationScope{}, false, fmt.Errorf("resolve deleted comment scope: %w", err)
		}
	case (strings.HasPrefix(event.Topic, "label.") && payload.RepositoryID != "") ||
		(strings.HasPrefix(event.Topic, "branch_rule.") && payload.RepositoryID != ""):
		if _, err := uuid.Parse(payload.RepositoryID); err != nil {
			return notificationScope{}, false, nil
		}
		err := transaction.QueryRow(ctx, `
			SELECT r.organization_id, r.id, o.slug, r.slug, r.visibility
			FROM repositories r JOIN organizations o ON o.id = r.organization_id
			WHERE r.id = $1
		`, payload.RepositoryID).Scan(&scope.OrganizationID, &scope.RepositoryID, &scope.OrganizationSlug,
			&scope.RepositorySlug, &scope.Visibility)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return notificationScope{}, false, nil
			}
			return notificationScope{}, false, fmt.Errorf("resolve deleted repository scope: %w", err)
		}
		if strings.HasPrefix(event.Topic, "label.") {
			scope.Title = payload.Name
		} else {
			scope.Title = payload.Pattern
		}
	default:
		return notificationScope{}, false, nil
	}
	scope.Href = notificationHref(scope)
	return scope, true, nil
}

func notificationEventID(event notificationEvent) (string, bool) {
	eventID := strings.SplitN(event.EventKey, ":", 2)[0]
	if _, err := uuid.Parse(eventID); err != nil {
		return "", false
	}
	return eventID, true
}

func (store *Store) notificationRecipients(
	ctx context.Context,
	transaction pgx.Tx,
	scope notificationScope,
	topic string,
) ([]string, error) {
	category := "repository"
	if scope.RepositoryID == "" {
		category = "organization"
	}
	if strings.HasPrefix(topic, "team.") {
		category = "team"
	}
	if category == "team" {
		rows, err := transaction.Query(ctx, `
			SELECT DISTINCT tm.user_id::text
			FROM team_memberships tm
			JOIN users recipient ON recipient.id = tm.user_id AND recipient.status = 'active'
			LEFT JOIN notification_preferences preferences ON preferences.user_id = tm.user_id
			WHERE tm.team_id = $1
			  AND COALESCE(preferences.in_app_enabled, true)
			  AND COALESCE(preferences.team_enabled, true)
		`, scope.TeamID)
		if err != nil {
			return nil, fmt.Errorf("list team notification recipients: %w", err)
		}
		return scanNotificationRecipients(rows)
	}
	if scope.RepositoryID == "" {
		rows, err := transaction.Query(ctx, `
			SELECT DISTINCT members.user_id::text
			FROM organization_memberships members
			JOIN users recipient ON recipient.id = members.user_id AND recipient.status = 'active'
			LEFT JOIN notification_preferences preferences ON preferences.user_id = members.user_id
			WHERE members.organization_id = $1
			  AND COALESCE(preferences.in_app_enabled, true)
		`, scope.OrganizationID)
		if err != nil {
			return nil, fmt.Errorf("list organization notification recipients: %w", err)
		}
		return scanNotificationRecipients(rows)
	}
	rows, err := transaction.Query(ctx, `
		WITH eligible AS (
			SELECT members.user_id
			FROM organization_memberships members
			JOIN users recipient ON recipient.id = members.user_id AND recipient.status = 'active'
			WHERE members.organization_id = $1 AND $3 = 'internal'
			UNION
			SELECT repository_members.user_id
			FROM repository_memberships repository_members
			JOIN users recipient ON recipient.id = repository_members.user_id
			WHERE repository_members.repository_id = $2
			  AND recipient.status = 'active'
			  AND $3 IN ('public', 'private')
			UNION
			SELECT team_members.user_id
			FROM team_memberships team_members
			JOIN team_repository_memberships team_repositories
			  ON team_repositories.team_id = team_members.team_id
			JOIN users recipient ON recipient.id = team_members.user_id
			WHERE team_repositories.repository_id = $2
			  AND team_repositories.active
			  AND recipient.status = 'active'
			  AND $3 IN ('public', 'private')
		)
		SELECT DISTINCT eligible.user_id::text
		FROM eligible
		LEFT JOIN notification_preferences preferences ON preferences.user_id = eligible.user_id
		WHERE COALESCE(preferences.in_app_enabled, true)
		  AND COALESCE(preferences.repository_enabled, true)
	`, scope.OrganizationID, scope.RepositoryID, scope.Visibility)
	if err != nil {
		return nil, fmt.Errorf("list notification recipients: %w", err)
	}
	return scanNotificationRecipients(rows)
}

func scanNotificationRecipients(rows pgx.Rows) ([]string, error) {
	defer rows.Close()
	recipients := make([]string, 0)
	for rows.Next() {
		var recipient string
		if err := rows.Scan(&recipient); err != nil {
			return nil, fmt.Errorf("scan notification recipient: %w", err)
		}
		recipients = append(recipients, recipient)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate notification recipients: %w", err)
	}
	return recipients, nil
}

type notificationText struct {
	title string
	body  string
	href  string
}

func notificationMessage(event notificationEvent, scope notificationScope) notificationText {
	var fields map[string]any
	_ = json.Unmarshal(event.Payload, &fields)
	title := scope.Title
	if value, ok := fields["title"].(string); ok && strings.TrimSpace(value) != "" {
		title = value
	}
	if strings.TrimSpace(title) == "" {
		title = strings.ReplaceAll(event.Topic, ".", " ")
	}
	body, _ := fields["body"].(string)
	if len([]rune(body)) > 500 {
		body = string([]rune(body)[:500])
	}
	return notificationText{title: title, body: body, href: scope.Href}
}

func notificationHref(scope notificationScope) string {
	if scope.RepositorySlug != "" && scope.IssueNumber != nil {
		return "/" + scope.OrganizationSlug + "/" + scope.RepositorySlug + "/issues"
	}
	if scope.RepositorySlug != "" && scope.MergeRequestNumber != nil {
		return "/" + scope.OrganizationSlug + "/" + scope.RepositorySlug + "/pulls"
	}
	if scope.RepositorySlug != "" {
		return "/" + scope.OrganizationSlug + "/" + scope.RepositorySlug
	}
	if scope.OrganizationSlug != "" {
		return "/organizations/" + scope.OrganizationSlug
	}
	return "/notifications"
}

func scanNotifications(rows pgx.Rows) ([]Notification, error) {
	items := make([]Notification, 0)
	for rows.Next() {
		var item Notification
		if err := rows.Scan(&item.ID, &item.Topic, &item.Title, &item.Body, &item.Href, &item.ReadAt,
			&item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan notification: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate notifications: %w", err)
	}
	return items, nil
}
