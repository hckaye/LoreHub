package webhooks

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const projectionBatchSize = 100

type deliveryEnvelope struct {
	DeliveryID string          `json:"deliveryId"`
	Event      string          `json:"event"`
	OccurredAt time.Time       `json:"occurredAt"`
	Repository envelopeRepo    `json:"repository"`
	Payload    json.RawMessage `json:"payload"`
}

type envelopeRepo struct {
	ID    string `json:"id"`
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

func (store *Store) Project(ctx context.Context) (int, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin webhook projection: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	events, err := claimSourceEvents(ctx, tx)
	if err != nil {
		return 0, err
	}
	created := 0
	for _, event := range events {
		count, err := store.projectEvent(ctx, tx, event)
		if err != nil {
			return 0, err
		}
		created += count
		if _, err := tx.Exec(ctx, `
			UPDATE webhook_projection_ledger
			SET status = 'processed', processed_at = now()
			WHERE source_event_id = $1 AND status = 'processing'
		`, event.ID); err != nil {
			return 0, fmt.Errorf("complete webhook event projection: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit webhook projection: %w", err)
	}
	return created, nil
}

func claimSourceEvents(ctx context.Context, tx pgx.Tx) ([]sourceEvent, error) {
	rows, err := tx.Query(ctx, `
		WITH candidates AS MATERIALIZED (
			SELECT event.id, event.topic, event.event_key, event.payload, event.created_at
			FROM outbox_events event
			LEFT JOIN webhook_projection_ledger ledger ON ledger.source_event_id = event.id
			WHERE event.topic ~ '^(actions|branch_rule|branch|issue_comment|issue_label|issue|label|milestone|'
			    'project|merge_request_comment|merge_request_review_request|merge_request_review|'
			    'merge_request_review_thread|'
			    'merge_request_review_comment|merge_request|merge_operation|release|repository|wiki)\.'
			  AND (
			      ledger.source_event_id IS NULL
			      OR (ledger.status = 'processing' AND ledger.claimed_at < now() - interval '5 minutes')
			  )
			ORDER BY event.created_at, event.id
			LIMIT $1
			FOR UPDATE OF event SKIP LOCKED
		), claimed AS (
			INSERT INTO webhook_projection_ledger (source_event_id, status, claimed_at)
			SELECT id, 'processing', now() FROM candidates
			ON CONFLICT (source_event_id) DO UPDATE SET
				status = 'processing', claimed_at = EXCLUDED.claimed_at, processed_at = NULL
			RETURNING source_event_id
		)
		SELECT candidate.id, candidate.topic, candidate.event_key,
		       candidate.payload, candidate.created_at
		FROM candidates candidate
		JOIN claimed ON claimed.source_event_id = candidate.id
		ORDER BY candidate.created_at, candidate.id
	`, projectionBatchSize)
	if err != nil {
		return nil, fmt.Errorf("claim webhook source events: %w", err)
	}
	defer rows.Close()
	events := make([]sourceEvent, 0)
	for rows.Next() {
		var event sourceEvent
		if err := rows.Scan(&event.ID, &event.Topic, &event.EventKey, &event.Payload, &event.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan webhook source event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate webhook source events: %w", err)
	}
	return events, nil
}

func (store *Store) projectEvent(ctx context.Context, tx pgx.Tx, event sourceEvent) (int, error) {
	kind, supported := eventKind(event.Topic)
	if !supported {
		return 0, nil
	}
	repository, found, err := resolveEventRepository(ctx, tx, event)
	if err != nil || !found {
		return 0, err
	}
	rows, err := tx.Query(ctx, `
		SELECT id
		FROM repository_webhooks
		WHERE repository_id = $1 AND active AND created_at <= $2 AND $3 = ANY(events)
		ORDER BY created_at, id
	`, repository.ID, event.CreatedAt, kind)
	if err != nil {
		return 0, fmt.Errorf("find webhook event subscribers: %w", err)
	}
	webhookIDs := make([]string, 0)
	for rows.Next() {
		var webhookID string
		if err := rows.Scan(&webhookID); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan webhook event subscriber: %w", err)
		}
		webhookIDs = append(webhookIDs, webhookID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("iterate webhook event subscribers: %w", err)
	}
	rows.Close()
	created := 0
	for _, webhookID := range webhookIDs {
		deliveryID := uuid.NewString()
		body, err := json.Marshal(deliveryEnvelope{
			DeliveryID: deliveryID,
			Event:      event.Topic,
			OccurredAt: event.CreatedAt.UTC(),
			Repository: envelopeRepo{ID: repository.ID, Owner: repository.Owner, Name: repository.Slug},
			Payload:    json.RawMessage(event.Payload),
		})
		if err != nil {
			return 0, fmt.Errorf("encode webhook delivery: %w", err)
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO webhook_deliveries (
				id, webhook_id, source_event_id, event_name, request_body, next_attempt_at, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, now(), now(), now())
			ON CONFLICT (webhook_id, source_event_id) DO NOTHING
		`, deliveryID, webhookID, event.ID, event.Topic, body)
		if err != nil {
			return 0, fmt.Errorf("create webhook delivery: %w", err)
		}
		created += int(tag.RowsAffected())
	}
	return created, nil
}
