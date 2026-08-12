package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type NotificationEmailClaim struct {
	DeliveryID     string
	NotificationID string
	Recipient      string
	Locale         string
	Topic          string
	Title          string
	Body           string
	Href           string
	Attempt        int
}

func (store *Store) ProjectNotifications(ctx context.Context) (int, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin notification projection: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	projected, err := store.syncNotifications(ctx, transaction)
	if err != nil {
		return 0, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit notification projection: %w", err)
	}
	return projected, nil
}

func (store *Store) ClaimNotificationEmail(
	ctx context.Context,
	workerID string,
	lease time.Duration,
) (*NotificationEmailClaim, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin notification email claim: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	accessClause := notificationCurrentAccessClause("notification", "notification.recipient_id::text")
	if _, err := transaction.Exec(ctx, `
		UPDATE notification_email_deliveries delivery
		SET status = 'cancelled', lease_owner = NULL, lease_expires_at = NULL,
		    last_error = '', updated_at = now()
		FROM notifications notification
		JOIN users recipient ON recipient.id = notification.recipient_id
		LEFT JOIN notification_preferences preferences
		  ON preferences.user_id = notification.recipient_id
		WHERE delivery.notification_id = notification.id
		  AND (
		      delivery.status IN ('queued', 'failed')
		      OR (delivery.status = 'delivering' AND delivery.lease_expires_at < now())
		  )
		  AND (
		      NOT notification.email_enabled
		      OR recipient.status <> 'active'
		      OR recipient.email IS NULL
		      OR btrim(recipient.email) = ''
		      OR NOT COALESCE(preferences.email_enabled, false)
		      OR (
		          notification.scope_kind = 'repository'
		          AND NOT COALESCE(preferences.repository_enabled, true)
		      )
		      OR (
		          notification.scope_kind = 'team'
		          AND NOT COALESCE(preferences.team_enabled, true)
		      )
		      OR NOT `+accessClause+`
		  )
	`); err != nil {
		return nil, fmt.Errorf("cancel ineligible notification emails: %w", err)
	}
	var claim NotificationEmailClaim
	err = transaction.QueryRow(ctx, `
		WITH candidate AS MATERIALIZED (
			SELECT delivery.id
			FROM notification_email_deliveries delivery
			JOIN notifications notification ON notification.id = delivery.notification_id
			JOIN users recipient
			  ON recipient.id = notification.recipient_id
			 AND recipient.status = 'active'
			 AND recipient.email IS NOT NULL
			 AND btrim(recipient.email) <> ''
			JOIN notification_preferences preferences
			  ON preferences.user_id = notification.recipient_id
			 AND preferences.email_enabled
			WHERE notification.email_enabled
			  AND `+accessClause+`
			  AND (
			      notification.scope_kind <> 'repository'
			      OR preferences.repository_enabled
			  )
			  AND (
			      notification.scope_kind <> 'team'
			      OR preferences.team_enabled
			  )
			  AND (
			      delivery.status IN ('queued', 'failed')
			      AND delivery.next_attempt_at <= now()
			      OR delivery.status = 'delivering' AND delivery.lease_expires_at < now()
			  )
			ORDER BY delivery.next_attempt_at, delivery.created_at, delivery.id
			LIMIT 1
			FOR UPDATE OF delivery SKIP LOCKED
		), claimed AS (
			UPDATE notification_email_deliveries delivery
			SET status = 'delivering', lease_owner = $1, lease_expires_at = now() + $2::interval,
			    attempt_count = delivery.attempt_count + 1, updated_at = now(), last_error = ''
			FROM candidate
			WHERE delivery.id = candidate.id
			RETURNING delivery.*
		)
		SELECT claimed.id, notification.id, recipient.email,
		       CASE WHEN recipient.locale = 'ja' THEN 'ja' ELSE 'en' END,
		       notification.topic, notification.title, notification.body,
		       notification.href, claimed.attempt_count
		FROM claimed
		JOIN notifications notification ON notification.id = claimed.notification_id
		JOIN users recipient ON recipient.id = notification.recipient_id
	`, workerID, lease.String()).Scan(
		&claim.DeliveryID, &claim.NotificationID, &claim.Recipient, &claim.Locale,
		&claim.Topic, &claim.Title, &claim.Body, &claim.Href, &claim.Attempt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := transaction.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit empty notification email claim: %w", err)
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim notification email: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit notification email claim: %w", err)
	}
	return &claim, nil
}

func (store *Store) CompleteNotificationEmail(
	ctx context.Context,
	workerID string,
	claim NotificationEmailClaim,
	maxAttempts int,
	succeeded bool,
	retryAt time.Time,
	deliveryError string,
) error {
	status := "sent"
	var sentAt *time.Time
	finishedAt := time.Now().UTC()
	if succeeded {
		sentAt = &finishedAt
		deliveryError = ""
	} else if claim.Attempt >= maxAttempts {
		status = "exhausted"
	} else {
		status = "failed"
	}
	deliveryError = safeNotificationEmailError(deliveryError)
	tag, err := store.pool.Exec(ctx, `
		UPDATE notification_email_deliveries
		SET status = $1, next_attempt_at = $2, lease_owner = NULL, lease_expires_at = NULL,
		    sent_at = $3, last_error = $4, updated_at = now()
		WHERE id = $5 AND notification_id = $6
		  AND status = 'delivering' AND lease_owner = $7
	`, status, retryAt, sentAt, deliveryError, claim.DeliveryID, claim.NotificationID, workerID)
	if err != nil {
		return fmt.Errorf("complete notification email: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return errors.New("notification email lease is no longer owned by this worker")
	}
	return nil
}

func safeNotificationEmailError(value string) string {
	value = strings.ToValidUTF8(value, "�")
	value = strings.Map(func(character rune) rune {
		if character == '\n' || character == '\t' || character >= ' ' {
			return character
		}
		return -1
	}, value)
	for len(value) > 1024 {
		_, size := utf8.DecodeLastRuneInString(value)
		value = value[:len(value)-size]
	}
	return value
}
