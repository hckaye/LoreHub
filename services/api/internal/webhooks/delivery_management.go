package webhooks

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func (store *Store) ListDeliveries(
	ctx context.Context,
	actor platform.User,
	owner string,
	repository string,
	webhookID string,
	limit int,
) ([]Delivery, error) {
	if _, err := uuid.Parse(webhookID); err != nil {
		return nil, invalid("webhook ID is invalid")
	}
	if limit < 1 || limit > 100 {
		limit = 30
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("begin webhook delivery list: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	reference, err := managedRepository(ctx, tx, actor.ID, owner, repository, false)
	if err != nil {
		return nil, err
	}
	if err := requireWebhook(ctx, tx, reference.ID, webhookID); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
		SELECT id, event_name, status, attempt_count, response_status,
		       response_body, last_error, delivered_at, created_at, updated_at
		FROM webhook_deliveries
		WHERE webhook_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2
	`, webhookID, limit)
	if err != nil {
		return nil, fmt.Errorf("list webhook deliveries: %w", err)
	}
	defer rows.Close()
	deliveries := make([]Delivery, 0)
	for rows.Next() {
		delivery, scanErr := scanDelivery(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		deliveries = append(deliveries, delivery)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate webhook deliveries: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit webhook delivery list: %w", err)
	}
	return deliveries, nil
}

func (store *Store) DeliveryDetail(
	ctx context.Context,
	actor platform.User,
	owner string,
	repository string,
	webhookID string,
	deliveryID string,
) (DeliveryDetail, error) {
	if _, err := uuid.Parse(webhookID); err != nil {
		return DeliveryDetail{}, invalid("webhook ID is invalid")
	}
	if _, err := uuid.Parse(deliveryID); err != nil {
		return DeliveryDetail{}, invalid("delivery ID is invalid")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return DeliveryDetail{}, fmt.Errorf("begin webhook delivery detail: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	reference, err := managedRepository(ctx, tx, actor.ID, owner, repository, false)
	if err != nil {
		return DeliveryDetail{}, err
	}
	if err := requireWebhook(ctx, tx, reference.ID, webhookID); err != nil {
		return DeliveryDetail{}, err
	}
	delivery, err := scanDelivery(tx.QueryRow(ctx, `
		SELECT id, event_name, status, attempt_count, response_status,
		       response_body, last_error, delivered_at, created_at, updated_at
		FROM webhook_deliveries
		WHERE id = $1 AND webhook_id = $2
	`, deliveryID, webhookID))
	if errors.Is(err, pgx.ErrNoRows) {
		return DeliveryDetail{}, ErrNotFound
	}
	if err != nil {
		return DeliveryDetail{}, err
	}
	rows, err := tx.Query(ctx, `
		SELECT attempt_number, started_at, finished_at, response_status,
		       response_body, error_message
		FROM webhook_delivery_attempts
		WHERE delivery_id = $1
		ORDER BY attempt_number DESC
	`, deliveryID)
	if err != nil {
		return DeliveryDetail{}, fmt.Errorf("list webhook delivery attempts: %w", err)
	}
	defer rows.Close()
	attempts := make([]DeliveryAttempt, 0)
	for rows.Next() {
		var attempt DeliveryAttempt
		if err := rows.Scan(
			&attempt.AttemptNumber, &attempt.StartedAt, &attempt.FinishedAt,
			&attempt.ResponseStatus, &attempt.ResponseBody, &attempt.ErrorMessage,
		); err != nil {
			return DeliveryDetail{}, fmt.Errorf("scan webhook delivery attempt: %w", err)
		}
		attempts = append(attempts, attempt)
	}
	if err := rows.Err(); err != nil {
		return DeliveryDetail{}, fmt.Errorf("iterate webhook delivery attempts: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return DeliveryDetail{}, fmt.Errorf("commit webhook delivery detail: %w", err)
	}
	return DeliveryDetail{Delivery: delivery, Attempts: attempts}, nil
}

func (store *Store) Redeliver(
	ctx context.Context,
	actor platform.User,
	owner string,
	repository string,
	webhookID string,
	deliveryID string,
) error {
	if _, err := uuid.Parse(webhookID); err != nil {
		return invalid("webhook ID is invalid")
	}
	if _, err := uuid.Parse(deliveryID); err != nil {
		return invalid("delivery ID is invalid")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin webhook redelivery: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	reference, err := managedRepository(ctx, tx, actor.ID, owner, repository, true)
	if err != nil {
		return err
	}
	if err := requireWebhook(ctx, tx, reference.ID, webhookID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE webhook_deliveries
		SET status = 'queued', automatic_attempts = 0, next_attempt_at = now(),
		    response_status = NULL, response_body = '', last_error = '', delivered_at = NULL,
		    lease_owner = NULL, lease_expires_at = NULL, updated_at = now()
		WHERE id = $1 AND webhook_id = $2 AND status IN ('succeeded', 'failed', 'exhausted')
	`, deliveryID, webhookID)
	if err != nil {
		return fmt.Errorf("queue webhook redelivery: %w", err)
	}
	if tag.RowsAffected() != 1 {
		var exists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM webhook_deliveries WHERE id = $1 AND webhook_id = $2)
		`, deliveryID, webhookID).Scan(&exists); err != nil {
			return fmt.Errorf("check webhook delivery: %w", err)
		}
		if !exists {
			return ErrNotFound
		}
		return fmt.Errorf("delivery is already queued: %w", ErrConflict)
	}
	if err := recordAudit(ctx, tx, actor.ID, reference, "webhook.redeliver", deliveryID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit webhook redelivery: %w", err)
	}
	return nil
}

func requireWebhook(ctx context.Context, tx pgx.Tx, repositoryID string, webhookID string) error {
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM repository_webhooks WHERE id = $1 AND repository_id = $2
		)
	`, webhookID, repositoryID).Scan(&exists); err != nil {
		return fmt.Errorf("find repository webhook: %w", err)
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func scanDelivery(row webhookRow) (Delivery, error) {
	var delivery Delivery
	if err := row.Scan(
		&delivery.ID, &delivery.Event, &delivery.Status, &delivery.AttemptCount,
		&delivery.ResponseStatus, &delivery.ResponseBody, &delivery.LastError,
		&delivery.DeliveredAt, &delivery.CreatedAt, &delivery.UpdatedAt,
	); err != nil {
		return Delivery{}, fmt.Errorf("scan webhook delivery: %w", err)
	}
	return delivery, nil
}
