package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
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
	Kind               string
	OrganizationID     string
	RepositoryID       string
	TeamID             string
	OrganizationSlug   string
	RepositorySlug     string
	Visibility         string
	IssueNumber        *int64
	MergeRequestNumber *int64
	DiscussionNumber   *int64
	Revision           string
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
	if err := store.pruneInaccessibleNotifications(ctx, transaction, actor.ID); err != nil {
		return NotificationPage{}, err
	}
	var total int64
	if err := transaction.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM notifications n
		WHERE n.recipient_id = $1
		  AND `+notificationCurrentAccessClause("n", "$1")+`
		  AND ($2 = false OR n.read_at IS NULL)
	`, actor.ID, unreadOnly).Scan(&total); err != nil {
		return NotificationPage{}, fmt.Errorf("count notifications: %w", err)
	}
	rows, err := transaction.Query(ctx, `
		SELECT n.id, n.topic, n.title, n.body, n.href, n.read_at, n.created_at
		FROM notifications n
		WHERE n.recipient_id = $1
		  AND `+notificationCurrentAccessClause("n", "$1")+`
		  AND ($2 = false OR n.read_at IS NULL)
		ORDER BY n.created_at DESC, n.id DESC
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

func (store *Store) pruneInaccessibleNotifications(
	ctx context.Context,
	transaction pgx.Tx,
	userID string,
) error {
	_, err := transaction.Exec(ctx, `
		WITH stale AS MATERIALIZED (
			SELECT n.id
			FROM notifications n
			WHERE n.recipient_id = $1
			  AND (
			      n.scope_kind IS NOT NULL
			      OR (n.scope_organization_id IS NULL AND n.scope_repository_id IS NULL AND n.scope_team_id IS NULL)
			  )
			  AND NOT `+notificationCurrentAccessClause("n", "$1")+`
			ORDER BY n.created_at ASC, n.id ASC
			LIMIT `+fmt.Sprint(notificationProjectionBatchSize)+`
			FOR UPDATE OF n SKIP LOCKED
		)
		DELETE FROM notifications n
		USING stale
		WHERE n.id = stale.id AND n.recipient_id = $1
	`, userID)
	if err != nil {
		return fmt.Errorf("prune inaccessible notifications: %w", err)
	}
	return nil
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
	query := fmt.Sprintf(`
		UPDATE notifications SET read_at = COALESCE(read_at, now())
		WHERE id = $1 AND recipient_id = $2
		  AND %s
	`, notificationCurrentAccessClause("notifications", "$2"))
	tag, err := transaction.Exec(ctx, query, notificationID, actor.ID)
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
	query := fmt.Sprintf(`
		UPDATE notifications SET read_at = now()
		WHERE recipient_id = $1 AND read_at IS NULL
		  AND %s
	`, notificationCurrentAccessClause("notifications", "$1"))
	tag, err := transaction.Exec(ctx, query, actor.ID)
	if err != nil {
		return fmt.Errorf("mark all notifications read: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if err := transaction.Commit(ctx); err != nil {
			return fmt.Errorf("commit empty all notifications read update: %w", err)
		}
		return nil
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
			    'discussion.created', 'discussion.updated', 'discussion.comment.created',
			    'discussion.comment.updated', 'discussion.comment.deleted',
			    'discussion.answer.updated',
			    'merge_request.created', 'merge_request.updated',
			    'merge_request_comment.created', 'merge_request_comment.updated',
			    'merge_request_comment.deleted',
			    'merge_request_review.created', 'merge_request_review.updated',
			    'merge_request_review_request.created',
			    'merge_request_review_thread.created', 'merge_request_review_thread.resolved',
			    'merge_request_review_thread.unresolved',
			    'merge_request_review_comment.created', 'merge_request_review_comment.updated',
			    'merge_request_review_comment.deleted',
			    'revision_comment.created', 'revision_comment.updated', 'revision_comment.deleted',
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
			preferences, err := store.notificationRecipients(ctx, transaction, scope, event)
			if err != nil {
				return err
			}
			sort.Strings(preferences)
			message := notificationMessage(event, scope)
			for _, recipient := range preferences {
				var activeRecipient string
				if err := transaction.QueryRow(ctx, `
						SELECT id::text FROM users WHERE id = $1 AND status = 'active' FOR UPDATE
					`, recipient).Scan(&activeRecipient); errors.Is(err, pgx.ErrNoRows) {
					continue
				} else if err != nil {
					return fmt.Errorf("lock notification recipient: %w", err)
				}
				_, err := transaction.Exec(ctx, `
					INSERT INTO notifications (
					    id, recipient_id, source_event_id, topic, title, body, href,
					    scope_kind, scope_organization_id, scope_repository_id, scope_team_id,
					    scope_visibility, created_at
					) VALUES ($1, $2, $3, $4, $5, $6, $7,
					    NULLIF($8, ''), NULLIF($9, '')::uuid, NULLIF($10, '')::uuid,
					    NULLIF($11, '')::uuid, NULLIF($12, ''), $13)
					ON CONFLICT (recipient_id, source_event_id) DO UPDATE SET
						scope_kind = EXCLUDED.scope_kind,
						scope_organization_id = EXCLUDED.scope_organization_id,
						scope_repository_id = EXCLUDED.scope_repository_id,
						scope_team_id = EXCLUDED.scope_team_id,
						scope_visibility = EXCLUDED.scope_visibility
				`, uuid.NewString(), recipient, event.ID, event.Topic, message.title, message.body, message.href,
					scope.Kind, scope.OrganizationID, scope.RepositoryID, scope.TeamID, scope.Visibility, event.CreatedAt)
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
	case strings.HasPrefix(event.Topic, "revision_comment."):
		return store.resolveRevisionCommentNotificationScope(ctx, transaction, event)
	case strings.HasPrefix(event.Topic, "organization."):
		err = transaction.QueryRow(ctx, `
			SELECT id, '', slug, '', NULL, NULL, display_name
			FROM organizations WHERE id = split_part($1, ':', 1)::uuid AND active
		`, eventID).Scan(&scope.OrganizationID, &scope.RepositoryID, &scope.OrganizationSlug,
			&scope.RepositorySlug, &scope.IssueNumber, &scope.MergeRequestNumber, &scope.Title)
	case strings.HasPrefix(event.Topic, "repository."):
		err = transaction.QueryRow(ctx, `
			SELECT r.organization_id, r.id, o.slug, r.slug, r.visibility, NULL, NULL, r.display_name
			FROM repositories r JOIN organizations o ON o.id = r.organization_id AND o.active
			WHERE r.id = split_part($1, ':', 1)::uuid
			  AND r.lifecycle_state = 'active'
		`, eventID).Scan(&scope.OrganizationID, &scope.RepositoryID, &scope.OrganizationSlug,
			&scope.RepositorySlug, &scope.Visibility, &scope.IssueNumber, &scope.MergeRequestNumber,
			&scope.Title)
	case strings.HasPrefix(event.Topic, "issue_comment."):
		err = transaction.QueryRow(ctx, `
			SELECT r.organization_id, r.id, o.slug, r.slug, r.visibility, i.number, NULL, i.title
			FROM issue_comments c
			JOIN issues i ON i.id = c.issue_id
			JOIN repositories r ON r.id = i.repository_id
			  AND r.lifecycle_state = 'active'
			JOIN organizations o ON o.id = r.organization_id AND o.active
			WHERE c.id = split_part($1, ':', 1)::uuid
		`, eventID).Scan(&scope.OrganizationID, &scope.RepositoryID, &scope.OrganizationSlug,
			&scope.RepositorySlug, &scope.Visibility, &scope.IssueNumber, &scope.MergeRequestNumber,
			&scope.Title)
	case strings.HasPrefix(event.Topic, "issue."):
		err = transaction.QueryRow(ctx, `
			SELECT r.organization_id, r.id, o.slug, r.slug, r.visibility, i.number, NULL, i.title
			FROM issues i JOIN repositories r ON r.id = i.repository_id
			  AND r.lifecycle_state = 'active'
			JOIN organizations o ON o.id = r.organization_id AND o.active
			WHERE i.id = split_part($1, ':', 1)::uuid
		`, eventID).Scan(&scope.OrganizationID, &scope.RepositoryID, &scope.OrganizationSlug,
			&scope.RepositorySlug, &scope.Visibility, &scope.IssueNumber, &scope.MergeRequestNumber,
			&scope.Title)
	case strings.HasPrefix(event.Topic, "discussion.comment."):
		err = transaction.QueryRow(ctx, `
			SELECT r.organization_id, r.id, o.slug, r.slug, r.visibility,
			       NULL, NULL, discussion.number, discussion.title
			FROM discussion_comments comment
			JOIN discussions discussion ON discussion.id = comment.discussion_id
			JOIN repositories r ON r.id = discussion.repository_id
			  AND r.lifecycle_state = 'active'
			JOIN organizations o ON o.id = r.organization_id AND o.active
			WHERE comment.id = split_part($1, ':', 1)::uuid
		`, eventID).Scan(&scope.OrganizationID, &scope.RepositoryID, &scope.OrganizationSlug,
			&scope.RepositorySlug, &scope.Visibility, &scope.IssueNumber, &scope.MergeRequestNumber,
			&scope.DiscussionNumber, &scope.Title)
	case strings.HasPrefix(event.Topic, "discussion."):
		err = transaction.QueryRow(ctx, `
			SELECT r.organization_id, r.id, o.slug, r.slug, r.visibility,
			       NULL, NULL, discussion.number, discussion.title
			FROM discussions discussion
			JOIN repositories r ON r.id = discussion.repository_id
			  AND r.lifecycle_state = 'active'
			JOIN organizations o ON o.id = r.organization_id AND o.active
			WHERE discussion.id = split_part($1, ':', 1)::uuid
		`, eventID).Scan(&scope.OrganizationID, &scope.RepositoryID, &scope.OrganizationSlug,
			&scope.RepositorySlug, &scope.Visibility, &scope.IssueNumber, &scope.MergeRequestNumber,
			&scope.DiscussionNumber, &scope.Title)
	case strings.HasPrefix(event.Topic, "merge_request_comment."):
		err = transaction.QueryRow(ctx, `
			SELECT r.organization_id, r.id, o.slug, r.slug, r.visibility, NULL, mr.number, mr.title
			FROM merge_request_comments comment
			JOIN merge_requests mr ON mr.id = comment.merge_request_id
			JOIN repositories r ON r.id = mr.repository_id
			  AND r.lifecycle_state = 'active'
			JOIN organizations o ON o.id = r.organization_id AND o.active
			WHERE comment.id = split_part($1, ':', 1)::uuid
		`, eventID).Scan(&scope.OrganizationID, &scope.RepositoryID, &scope.OrganizationSlug,
			&scope.RepositorySlug, &scope.Visibility, &scope.IssueNumber, &scope.MergeRequestNumber,
			&scope.Title)
	case strings.HasPrefix(event.Topic, "merge_request_review_request."):
		err = transaction.QueryRow(ctx, `
			SELECT r.organization_id, r.id, o.slug, r.slug, r.visibility, NULL, mr.number, mr.title
			FROM merge_request_review_requests request
			JOIN merge_requests mr ON mr.id = request.merge_request_id
			JOIN repositories r ON r.id = request.repository_id
			  AND r.lifecycle_state = 'active'
			JOIN organizations o ON o.id = request.organization_id AND o.active
			WHERE request.id = split_part($1, ':', 1)::uuid
		`, eventID).Scan(&scope.OrganizationID, &scope.RepositoryID, &scope.OrganizationSlug,
			&scope.RepositorySlug, &scope.Visibility, &scope.IssueNumber, &scope.MergeRequestNumber,
			&scope.Title)
	case strings.HasPrefix(event.Topic, "merge_request_review."):
		err = transaction.QueryRow(ctx, `
			SELECT r.organization_id, r.id, o.slug, r.slug, r.visibility, NULL, mr.number, mr.title
			FROM merge_request_reviews review
			JOIN merge_requests mr ON mr.id = review.merge_request_id
			JOIN repositories r ON r.id = mr.repository_id
			  AND r.lifecycle_state = 'active'
			JOIN organizations o ON o.id = r.organization_id AND o.active
			WHERE review.id = split_part($1, ':', 1)::uuid
		`, eventID).Scan(&scope.OrganizationID, &scope.RepositoryID, &scope.OrganizationSlug,
			&scope.RepositorySlug, &scope.Visibility, &scope.IssueNumber, &scope.MergeRequestNumber,
			&scope.Title)
	case strings.HasPrefix(event.Topic, "merge_request_review_thread."),
		strings.HasPrefix(event.Topic, "merge_request_review_comment."):
		err = transaction.QueryRow(ctx, `
			SELECT r.organization_id, r.id, o.slug, r.slug, r.visibility, NULL, mr.number, mr.title
			FROM merge_request_review_threads thread
			JOIN merge_requests mr ON mr.id = thread.merge_request_id
			JOIN repositories r ON r.id = mr.repository_id
			  AND r.lifecycle_state = 'active'
			JOIN organizations o ON o.id = r.organization_id AND o.active
			WHERE thread.id = split_part($1, ':', 1)::uuid
		`, eventID).Scan(&scope.OrganizationID, &scope.RepositoryID, &scope.OrganizationSlug,
			&scope.RepositorySlug, &scope.Visibility, &scope.IssueNumber, &scope.MergeRequestNumber,
			&scope.Title)
	case strings.HasPrefix(event.Topic, "merge_request."):
		err = transaction.QueryRow(ctx, `
			SELECT r.organization_id, r.id, o.slug, r.slug, r.visibility, NULL, mr.number, mr.title
			FROM merge_requests mr JOIN repositories r ON r.id = mr.repository_id
			  AND r.lifecycle_state = 'active'
			JOIN organizations o ON o.id = r.organization_id AND o.active
			WHERE mr.id = split_part($1, ':', 1)::uuid
		`, eventID).Scan(&scope.OrganizationID, &scope.RepositoryID, &scope.OrganizationSlug,
			&scope.RepositorySlug, &scope.Visibility, &scope.IssueNumber, &scope.MergeRequestNumber,
			&scope.Title)
	case strings.HasPrefix(event.Topic, "label."):
		err = transaction.QueryRow(ctx, `
			SELECT r.organization_id, r.id, o.slug, r.slug, r.visibility, NULL, NULL, l.name
			FROM labels l JOIN repositories r ON r.id = l.repository_id
			  AND r.lifecycle_state = 'active'
			JOIN organizations o ON o.id = r.organization_id AND o.active
			WHERE l.id = split_part($1, ':', 1)::uuid
		`, eventID).Scan(&scope.OrganizationID, &scope.RepositoryID, &scope.OrganizationSlug,
			&scope.RepositorySlug, &scope.Visibility, &scope.IssueNumber, &scope.MergeRequestNumber,
			&scope.Title)
	case strings.HasPrefix(event.Topic, "branch_rule."):
		err = transaction.QueryRow(ctx, `
			SELECT r.organization_id, r.id, o.slug, r.slug, r.visibility, NULL, NULL, br.pattern
			FROM branch_rules br JOIN repositories r ON r.id = br.repository_id
			  AND r.lifecycle_state = 'active'
			JOIN organizations o ON o.id = r.organization_id AND o.active
			WHERE br.id = split_part($1, ':', 1)::uuid
		`, eventID).Scan(&scope.OrganizationID, &scope.RepositoryID, &scope.OrganizationSlug,
			&scope.RepositorySlug, &scope.Visibility, &scope.IssueNumber, &scope.MergeRequestNumber,
			&scope.Title)
	case strings.HasPrefix(event.Topic, "team."):
		err = transaction.QueryRow(ctx, `
			SELECT o.id, '', o.slug, '', NULL, NULL, t.display_name
			FROM teams t JOIN organizations o ON o.id = t.organization_id AND o.active
			WHERE t.id = split_part($1, ':', 1)::uuid AND t.active
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
		scope.Kind = "team"
	} else if scope.RepositoryID != "" {
		scope.Kind = "repository"
	} else {
		scope.Kind = "organization"
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
		IssueID        string `json:"issueId"`
		MergeRequestID string `json:"mergeRequestId"`
		RepositoryID   string `json:"repositoryId"`
		Name           string `json:"name"`
		Pattern        string `json:"pattern"`
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
			  AND r.lifecycle_state = 'active'
			JOIN organizations o ON o.id = r.organization_id AND o.active
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
		scope.Kind = "repository"
	case strings.HasPrefix(event.Topic, "merge_request_comment.") && payload.MergeRequestID != "":
		if _, err := uuid.Parse(payload.MergeRequestID); err != nil {
			return notificationScope{}, false, nil
		}
		err := transaction.QueryRow(ctx, `
			SELECT r.organization_id, r.id, o.slug, r.slug, r.visibility, NULL, mr.number, mr.title
			FROM merge_requests mr JOIN repositories r ON r.id = mr.repository_id
			  AND r.lifecycle_state = 'active'
			JOIN organizations o ON o.id = r.organization_id AND o.active
			WHERE mr.id = $1
		`, payload.MergeRequestID).Scan(&scope.OrganizationID, &scope.RepositoryID,
			&scope.OrganizationSlug, &scope.RepositorySlug, &scope.Visibility,
			&scope.IssueNumber, &scope.MergeRequestNumber, &scope.Title)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return notificationScope{}, false, nil
			}
			return notificationScope{}, false, fmt.Errorf("resolve deleted pull request comment scope: %w", err)
		}
		scope.Kind = "repository"
	case (strings.HasPrefix(event.Topic, "label.") && payload.RepositoryID != "") ||
		(strings.HasPrefix(event.Topic, "branch_rule.") && payload.RepositoryID != ""):
		if _, err := uuid.Parse(payload.RepositoryID); err != nil {
			return notificationScope{}, false, nil
		}
		err := transaction.QueryRow(ctx, `
			SELECT r.organization_id, r.id, o.slug, r.slug, r.visibility
			FROM repositories r JOIN organizations o ON o.id = r.organization_id AND o.active
			WHERE r.id = $1 AND r.lifecycle_state = 'active'
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
		scope.Kind = "repository"
	default:
		return notificationScope{}, false, nil
	}
	scope.Href = notificationHref(scope)
	return scope, true, nil
}

func (store *Store) resolveRevisionCommentNotificationScope(
	ctx context.Context,
	transaction pgx.Tx,
	event notificationEvent,
) (notificationScope, bool, error) {
	var payload struct {
		RepositoryID string `json:"repositoryId"`
		Comment      struct {
			Revision string `json:"revision"`
		} `json:"comment"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil || !validNotificationRevision(payload.Comment.Revision) {
		return notificationScope{}, false, nil
	}
	if _, err := uuid.Parse(payload.RepositoryID); err != nil {
		return notificationScope{}, false, nil
	}
	var scope notificationScope
	err := transaction.QueryRow(ctx, `
		SELECT r.organization_id, r.id, o.slug, r.slug, r.visibility
		FROM repositories r
		JOIN organizations o ON o.id = r.organization_id AND o.active
		WHERE r.id = $1 AND r.lifecycle_state = 'active'
	`, payload.RepositoryID).Scan(
		&scope.OrganizationID, &scope.RepositoryID, &scope.OrganizationSlug,
		&scope.RepositorySlug, &scope.Visibility,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return notificationScope{}, false, nil
	}
	if err != nil {
		return notificationScope{}, false, fmt.Errorf("resolve revision comment notification scope: %w", err)
	}
	scope.Kind = "repository"
	scope.Revision = payload.Comment.Revision
	scope.Title = "Revision " + payload.Comment.Revision[:12]
	scope.Href = notificationHref(scope)
	return scope, true, nil
}

func validNotificationRevision(value string) bool {
	return len(value) == 64 && strings.Trim(value, "0123456789abcdef") == ""
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
	event notificationEvent,
) ([]string, error) {
	if strings.HasPrefix(event.Topic, "merge_request_review_request.") {
		return reviewRequestNotificationRecipients(ctx, transaction, event)
	}
	topic := event.Topic
	if scope.Kind == "team" || strings.HasPrefix(topic, "team.") {
		rows, err := transaction.Query(ctx, `
			SELECT DISTINCT tm.user_id::text
			FROM team_memberships tm
			JOIN teams t ON t.id = tm.team_id AND t.active
			JOIN organizations o ON o.id = t.organization_id AND o.active
			JOIN organization_memberships om
			  ON om.organization_id = o.id AND om.user_id = tm.user_id AND om.active
			JOIN users recipient ON recipient.id = tm.user_id AND recipient.status = 'active'
			LEFT JOIN notification_preferences preferences ON preferences.user_id = tm.user_id
			WHERE tm.team_id = $1
			  AND tm.active
			  AND COALESCE(preferences.in_app_enabled, true)
			  AND COALESCE(preferences.team_enabled, true)
		`, scope.TeamID)
		if err != nil {
			return nil, fmt.Errorf("list team notification recipients: %w", err)
		}
		return scanNotificationRecipients(rows)
	}
	if scope.Kind == "organization" || scope.RepositoryID == "" {
		rows, err := transaction.Query(ctx, `
			SELECT DISTINCT members.user_id::text
			FROM organizations o
			JOIN organization_memberships members ON members.organization_id = o.id
			JOIN users recipient ON recipient.id = members.user_id AND recipient.status = 'active'
			LEFT JOIN notification_preferences preferences ON preferences.user_id = members.user_id
			WHERE o.id = $1 AND o.active
			  AND members.active
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
			JOIN organizations o ON o.id = members.organization_id AND o.active
			JOIN users recipient ON recipient.id = members.user_id AND recipient.status = 'active'
			WHERE members.organization_id = $1
			  AND members.active
			  AND ($3 = 'internal' OR ($3 = 'private' AND members.role = 'owner'))
			UNION
			SELECT repository_members.user_id
			FROM repository_memberships repository_members
			JOIN repositories r ON r.id = repository_members.repository_id
			  AND r.organization_id = $1 AND r.lifecycle_state = 'active'
			JOIN organizations o ON o.id = r.organization_id AND o.active
			JOIN users recipient ON recipient.id = repository_members.user_id
			WHERE repository_members.repository_id = $2
			  AND repository_members.active
			  AND recipient.status = 'active'
			  AND $3 IN ('public', 'internal', 'private')
			UNION
			SELECT team_members.user_id
			FROM team_memberships team_members
			JOIN team_repository_roles team_repositories
			  ON team_repositories.team_id = team_members.team_id
			 AND team_repositories.active
			JOIN teams t ON t.id = team_members.team_id AND t.active AND t.organization_id = $1
			JOIN organizations o ON o.id = t.organization_id AND o.active
			JOIN organization_memberships team_org_member
			  ON team_org_member.organization_id = o.id
			 AND team_org_member.user_id = team_members.user_id
			 AND team_org_member.active
			JOIN users recipient ON recipient.id = team_members.user_id
			WHERE team_repositories.repository_id = $2
			  AND team_members.active
			  AND recipient.status = 'active'
			  AND $3 IN ('public', 'internal', 'private')
			UNION
			SELECT watches.user_id
			FROM repository_watches watches
			JOIN repositories r ON r.id = watches.repository_id
			  AND r.organization_id = $1 AND r.lifecycle_state = 'active'
			JOIN organizations o ON o.id = r.organization_id AND o.active
			JOIN users recipient ON recipient.id = watches.user_id AND recipient.status = 'active'
			WHERE watches.repository_id = $2
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

func reviewRequestNotificationRecipients(
	ctx context.Context,
	transaction pgx.Tx,
	event notificationEvent,
) ([]string, error) {
	eventID, valid := notificationEventID(event)
	if !valid {
		return nil, nil
	}
	rows, err := transaction.Query(ctx, `
		SELECT DISTINCT recipient.id::text
		FROM merge_request_review_requests request
		JOIN users recipient
		  ON recipient.id = request.reviewer_user_id AND recipient.status = 'active'
		LEFT JOIN notification_preferences preferences ON preferences.user_id = recipient.id
		WHERE request.id = $1 AND request.removed_at IS NULL
		  AND COALESCE(preferences.in_app_enabled, true)
		UNION
		SELECT DISTINCT recipient.id::text
		FROM merge_request_review_requests request
		JOIN teams team ON team.id = request.reviewer_team_id AND team.active
		JOIN team_memberships membership ON membership.team_id = team.id AND membership.active
		JOIN organization_memberships organization_member
		  ON organization_member.organization_id = request.organization_id
		 AND organization_member.user_id = membership.user_id AND organization_member.active
		JOIN users recipient ON recipient.id = membership.user_id AND recipient.status = 'active'
		LEFT JOIN notification_preferences preferences ON preferences.user_id = recipient.id
		WHERE request.id = $1 AND request.removed_at IS NULL
		  AND COALESCE(preferences.in_app_enabled, true)
		  AND COALESCE(preferences.team_enabled, true)
	`, eventID)
	if err != nil {
		return nil, fmt.Errorf("list review request notification recipients: %w", err)
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
	if comment, ok := fields["comment"].(map[string]any); ok {
		if commentBody, ok := comment["body"].(string); ok {
			body = commentBody
		}
	}
	if len([]rune(body)) > 500 {
		body = string([]rune(body)[:500])
	}
	return notificationText{title: title, body: body, href: scope.Href}
}

func notificationHref(scope notificationScope) string {
	if scope.RepositorySlug != "" && scope.Revision != "" {
		return "/" + scope.OrganizationSlug + "/" + scope.RepositorySlug +
			"/commit?revision=" + url.QueryEscape(scope.Revision)
	}
	if scope.RepositorySlug != "" && scope.DiscussionNumber != nil {
		return "/" + scope.OrganizationSlug + "/" + scope.RepositorySlug +
			"/discussions/" + fmt.Sprint(*scope.DiscussionNumber)
	}
	if scope.RepositorySlug != "" && scope.IssueNumber != nil {
		return "/" + scope.OrganizationSlug + "/" + scope.RepositorySlug +
			"/issues/" + fmt.Sprint(*scope.IssueNumber)
	}
	if scope.RepositorySlug != "" && scope.MergeRequestNumber != nil {
		return "/" + scope.OrganizationSlug + "/" + scope.RepositorySlug +
			"/pulls/" + fmt.Sprint(*scope.MergeRequestNumber)
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
