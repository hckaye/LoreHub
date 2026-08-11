package webhooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	maxAutomaticAttempts  = 8
	maxResponseBodyBytes  = 64 << 10
	maxStoredResponse     = 4096
	maxStoredError        = 1024
	maxDeliveriesPerCycle = 25
)

type claimedDelivery struct {
	ID                string
	WebhookID         string
	RepositoryID      string
	URL               string
	Event             string
	Body              []byte
	AttemptCount      int
	AutomaticAttempts int
	Ciphertext        []byte
	Nonce             []byte
	KeyID             string
}

type deliveryResult struct {
	StartedAt    time.Time
	FinishedAt   time.Time
	Status       *int
	ResponseBody string
	ErrorMessage string
	Successful   bool
}

type Worker struct {
	store      *Store
	client     *http.Client
	workerID   string
	interval   time.Duration
	lease      time.Duration
	logger     *slog.Logger
	maxPerTick int
}

func NewWorker(
	store *Store,
	interval time.Duration,
	lease time.Duration,
	logger *slog.Logger,
) (*Worker, error) {
	if store == nil || store.pool == nil || store.target == nil {
		return nil, errors.New("webhook store is required")
	}
	if interval <= 0 || interval > time.Minute {
		return nil, errors.New("webhook poll interval must be between one nanosecond and one minute")
	}
	if lease < store.target.timeout || lease > 5*time.Minute {
		return nil, errors.New("webhook lease must cover the request timeout and be no longer than five minutes")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{
		store: store, client: store.target.Client(), workerID: uuid.NewString(),
		interval: interval, lease: lease, logger: logger, maxPerTick: maxDeliveriesPerCycle,
	}, nil
}

func (worker *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(worker.interval)
	defer ticker.Stop()
	for {
		worker.runCycle(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (worker *Worker) runCycle(ctx context.Context) {
	if _, err := worker.store.Project(ctx); err != nil && !errors.Is(err, context.Canceled) {
		worker.logger.Error("Webhook event projection failed", "error", err)
		return
	}
	for index := 0; index < worker.maxPerTick; index++ {
		claimed, err := worker.store.claimDelivery(ctx, worker.workerID, worker.lease)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				worker.logger.Error("Webhook delivery claim failed", "error", err)
			}
			return
		}
		if claimed == nil {
			return
		}
		result := worker.send(ctx, *claimed)
		if err := worker.store.completeDelivery(ctx, worker.workerID, *claimed, result); err != nil {
			if !errors.Is(err, context.Canceled) {
				worker.logger.Error("Webhook delivery completion failed", "error", err)
			}
			return
		}
	}
}

func (store *Store) claimDelivery(
	ctx context.Context,
	workerID string,
	lease time.Duration,
) (*claimedDelivery, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin webhook delivery claim: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var delivery claimedDelivery
	err = tx.QueryRow(ctx, `
		WITH candidate AS MATERIALIZED (
			SELECT delivery.id
			FROM webhook_deliveries delivery
			JOIN repository_webhooks webhook ON webhook.id = delivery.webhook_id AND webhook.active
			JOIN repositories repository
			  ON repository.id = webhook.repository_id
			 AND repository.lifecycle_state = 'active' AND repository.archived_at IS NULL
			JOIN organizations organization
			  ON organization.id = repository.organization_id AND organization.active
			WHERE (
			    delivery.status IN ('queued', 'failed') AND delivery.next_attempt_at <= now()
			) OR (
			    delivery.status = 'delivering' AND delivery.lease_expires_at < now()
			)
			ORDER BY delivery.next_attempt_at, delivery.created_at, delivery.id
			LIMIT 1
			FOR UPDATE OF delivery SKIP LOCKED
		), claimed AS (
			UPDATE webhook_deliveries delivery
			SET status = 'delivering', lease_owner = $1, lease_expires_at = now() + $2::interval,
			    attempt_count = delivery.attempt_count + 1,
			    automatic_attempts = delivery.automatic_attempts + 1,
			    updated_at = now()
			FROM candidate
			WHERE delivery.id = candidate.id
			RETURNING delivery.*
		)
		SELECT claimed.id, webhook.id, webhook.repository_id, webhook.url,
		       claimed.event_name, claimed.request_body, claimed.attempt_count,
		       claimed.automatic_attempts, webhook.secret_ciphertext,
		       webhook.secret_nonce, webhook.secret_key_id
		FROM claimed
		JOIN repository_webhooks webhook ON webhook.id = claimed.webhook_id
	`, workerID, lease.String()).Scan(
		&delivery.ID, &delivery.WebhookID, &delivery.RepositoryID, &delivery.URL,
		&delivery.Event, &delivery.Body, &delivery.AttemptCount, &delivery.AutomaticAttempts,
		&delivery.Ciphertext, &delivery.Nonce, &delivery.KeyID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit empty webhook delivery claim: %w", err)
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim webhook delivery: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit webhook delivery claim: %w", err)
	}
	return &delivery, nil
}

func (worker *Worker) send(ctx context.Context, delivery claimedDelivery) (result deliveryResult) {
	result.StartedAt = time.Now().UTC()
	defer func() { result.FinishedAt = time.Now().UTC() }()
	secret, err := worker.store.box.Open(
		delivery.WebhookID, delivery.RepositoryID,
		delivery.Ciphertext, delivery.Nonce, delivery.KeyID,
	)
	if err != nil {
		result.ErrorMessage = "webhook secret could not be decrypted"
		return result
	}
	defer clear(secret)
	if _, err := worker.store.target.Validate(ctx, delivery.URL); err != nil {
		result.ErrorMessage = "webhook target is unavailable"
		return result
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, delivery.URL, bytes.NewReader(delivery.Body))
	if err != nil {
		result.ErrorMessage = "webhook request could not be created"
		return result
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(delivery.Body)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "LoreHub-Hookshot/1.0")
	request.Header.Set("X-LoreHub-Delivery", delivery.ID)
	request.Header.Set("X-LoreHub-Event", delivery.Event)
	request.Header.Set("X-LoreHub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	response, err := worker.client.Do(request)
	if err != nil {
		result.ErrorMessage = "webhook target did not accept the request"
		return result
	}
	defer response.Body.Close()
	result.Status = &response.StatusCode
	encoded, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBodyBytes+1))
	if readErr != nil {
		result.ErrorMessage = "webhook response could not be read"
		return result
	}
	result.ResponseBody = safeStoredText(encoded, maxStoredResponse)
	result.Successful = response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices
	if !result.Successful {
		result.ErrorMessage = fmt.Sprintf("webhook target returned HTTP %d", response.StatusCode)
	}
	return result
}

func (store *Store) completeDelivery(
	ctx context.Context,
	workerID string,
	delivery claimedDelivery,
	result deliveryResult,
) error {
	if result.FinishedAt.IsZero() {
		result.FinishedAt = time.Now().UTC()
	}
	result.ResponseBody = safeStoredText([]byte(result.ResponseBody), maxStoredResponse)
	result.ErrorMessage = safeStoredText([]byte(result.ErrorMessage), maxStoredError)
	status := "succeeded"
	nextAttempt := result.FinishedAt
	var deliveredAt *time.Time
	if result.Successful {
		deliveredAt = &result.FinishedAt
	} else if delivery.AutomaticAttempts >= maxAutomaticAttempts {
		status = "exhausted"
	} else {
		status = "failed"
		nextAttempt = result.FinishedAt.Add(retryDelay(delivery.AutomaticAttempts))
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin webhook delivery completion: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	tag, err := tx.Exec(ctx, `
		UPDATE webhook_deliveries
		SET status = $4, next_attempt_at = $5, response_status = $6,
		    response_body = $7, last_error = $8, delivered_at = $9,
		    lease_owner = NULL, lease_expires_at = NULL, updated_at = $3
		WHERE id = $1 AND status = 'delivering' AND lease_owner = $2
		  AND attempt_count = $10
	`, delivery.ID, workerID, result.FinishedAt, status, nextAttempt, result.Status,
		result.ResponseBody, result.ErrorMessage, deliveredAt, delivery.AttemptCount)
	if err != nil {
		return fmt.Errorf("complete webhook delivery: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return errors.New("webhook delivery lease was lost")
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO webhook_delivery_attempts (
			id, delivery_id, attempt_number, started_at, finished_at,
			response_status, response_body, error_message
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, uuid.NewString(), delivery.ID, delivery.AttemptCount, result.StartedAt, result.FinishedAt,
		result.Status, result.ResponseBody, result.ErrorMessage)
	if err != nil {
		return fmt.Errorf("record webhook delivery attempt: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit webhook delivery completion: %w", err)
	}
	return nil
}

func retryDelay(attempt int) time.Duration {
	delays := []time.Duration{
		time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour,
		6 * time.Hour, 12 * time.Hour, 24 * time.Hour,
	}
	if attempt < 1 {
		return delays[0]
	}
	if attempt > len(delays) {
		return delays[len(delays)-1]
	}
	return delays[attempt-1]
}

func safeStoredText(value []byte, limit int) string {
	if len(value) > limit {
		value = value[:limit]
	}
	for !utf8.Valid(value) && len(value) > 0 {
		value = value[:len(value)-1]
	}
	result := strings.ToValidUTF8(string(value), "�")
	return strings.ReplaceAll(result, "\x00", "")
}
