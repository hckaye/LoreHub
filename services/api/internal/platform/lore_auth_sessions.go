package platform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/authz"
)

func (store *Store) CreateLoreAuthSession(
	ctx context.Context,
	id string,
	codeDigest []byte,
	clientStateDigest []byte,
	expiresAt time.Time,
) error {
	_, err := store.pool.Exec(ctx, `
		INSERT INTO lore_auth_sessions (id, session_code_digest, client_state_digest, expires_at)
		VALUES ($1, $2, $3, $4)
	`, id, codeDigest, clientStateDigest, expiresAt)
	if err != nil {
		return fmt.Errorf("create Lore auth session: %w", err)
	}
	return nil
}

func (store *Store) ConfirmLoreAuthSession(
	ctx context.Context,
	codeDigest []byte,
	userID string,
) error {
	command, err := store.pool.Exec(ctx, `
		UPDATE lore_auth_sessions
		SET user_id = $2, confirmed_at = now()
		WHERE session_code_digest = $1 AND expires_at > now()
		  AND consumed_at IS NULL AND confirmed_at IS NULL
	`, codeDigest, userID)
	if err != nil {
		return fmt.Errorf("confirm Lore auth session: %w", err)
	}
	if command.RowsAffected() != 1 {
		return authz.ErrSessionNotFound
	}
	return nil
}

func (store *Store) PollLoreAuthSession(
	ctx context.Context,
	codeDigest []byte,
	clientStateDigest []byte,
) (authz.AuthSessionPoll, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return authz.AuthSessionPoll{}, fmt.Errorf("begin Lore auth poll: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	var storedState []byte
	var userID *string
	var expiresAt, nextPollAt time.Time
	var consumedAt *time.Time
	err = transaction.QueryRow(ctx, `
		SELECT client_state_digest, user_id, expires_at, next_poll_at, consumed_at
		FROM lore_auth_sessions
		WHERE session_code_digest = $1
		FOR UPDATE
	`, codeDigest).Scan(&storedState, &userID, &expiresAt, &nextPollAt, &consumedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return authz.AuthSessionPoll{}, authz.ErrSessionNotFound
		}
		return authz.AuthSessionPoll{}, fmt.Errorf("read Lore auth session: %w", err)
	}
	if !bytes.Equal(storedState, clientStateDigest) {
		return authz.AuthSessionPoll{}, authz.ErrSessionState
	}
	now := time.Now().UTC()
	if !expiresAt.After(now) {
		return authz.AuthSessionPoll{}, authz.ErrSessionNotFound
	}
	if consumedAt != nil {
		return authz.AuthSessionPoll{Consumed: true}, authz.ErrSessionConsumed
	}
	if nextPollAt.After(now) {
		return authz.AuthSessionPoll{RetryAfter: time.Until(nextPollAt)}, authz.ErrSessionRateLimited
	}
	_, err = transaction.Exec(ctx, `
		UPDATE lore_auth_sessions
		SET poll_count = poll_count + 1, next_poll_at = $2
		WHERE session_code_digest = $1
	`, codeDigest, now.Add(time.Second))
	if err != nil {
		return authz.AuthSessionPoll{}, fmt.Errorf("update Lore auth poll: %w", err)
	}
	result := authz.AuthSessionPoll{}
	if userID != nil && *userID != "" {
		_, err = transaction.Exec(ctx, `
			UPDATE lore_auth_sessions
			SET consumed_at = $2
			WHERE session_code_digest = $1 AND consumed_at IS NULL
		`, codeDigest, now)
		if err != nil {
			return authz.AuthSessionPoll{}, fmt.Errorf("consume Lore auth session: %w", err)
		}
		result.UserID = *userID
		result.Ready = true
	}
	if err := transaction.Commit(ctx); err != nil {
		return authz.AuthSessionPoll{}, fmt.Errorf("commit Lore auth poll: %w", err)
	}
	return result, nil
}

var _ authz.SessionStore = (*Store)(nil)
