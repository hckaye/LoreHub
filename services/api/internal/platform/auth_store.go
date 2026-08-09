package platform

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/auth"
)

func (store *Store) CreateLoginTransaction(ctx context.Context, transaction auth.LoginTransaction) error {
	transactionID := transaction.ID
	if transactionID == "" {
		transactionID = uuid.NewString()
	}
	_, err := store.pool.Exec(ctx, `
		INSERT INTO login_transactions (
			id, state_digest, code_verifier_digest, nonce_digest, return_to, created_at, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, transactionID, transaction.StateDigest, transaction.CodeVerifierDigest, transaction.NonceDigest,
		transaction.ReturnTo, transaction.CreatedAt, transaction.ExpiresAt)
	if err != nil {
		return fmt.Errorf("create login transaction: %w", err)
	}
	return nil
}

func (store *Store) ConsumeLoginTransaction(
	ctx context.Context,
	stateDigest []byte,
	now time.Time,
) (auth.LoginTransaction, error) {
	var transaction auth.LoginTransaction
	err := store.pool.QueryRow(ctx, `
		UPDATE login_transactions
		SET used_at = $2
		WHERE state_digest = $1 AND used_at IS NULL AND expires_at > $2
		RETURNING id, state_digest, code_verifier_digest, nonce_digest, return_to, created_at, expires_at
	`, stateDigest, now).Scan(
		&transaction.ID,
		&transaction.StateDigest,
		&transaction.CodeVerifierDigest,
		&transaction.NonceDigest,
		&transaction.ReturnTo,
		&transaction.CreatedAt,
		&transaction.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.LoginTransaction{}, auth.ErrInvalidTransaction
	}
	if err != nil {
		return auth.LoginTransaction{}, fmt.Errorf("consume login transaction: %w", err)
	}
	return transaction, nil
}

func (store *Store) CreateSession(
	ctx context.Context,
	userID string,
	tokenDigest []byte,
	csrfDigest []byte,
	expiresAt time.Time,
) (auth.Session, error) {
	parsedUserID, err := uuid.Parse(userID)
	if err != nil {
		return auth.Session{}, fmt.Errorf("parse session user ID: %w", err)
	}
	var session auth.Session
	err = store.pool.QueryRow(ctx, `
		WITH inserted AS (
			INSERT INTO sessions (id, user_id, token_digest, csrf_token_digest, expires_at)
			SELECT $1, u.id, $3, $4, $5
			FROM users u
			WHERE u.id = $2 AND u.status = 'active'
			RETURNING id, user_id, csrf_token_digest, created_at, expires_at, last_seen_at
		)
		SELECT inserted.id, inserted.user_id, u.username, u.display_name, COALESCE(u.email, ''), u.locale,
		       inserted.csrf_token_digest, inserted.created_at, inserted.expires_at, inserted.last_seen_at
		FROM inserted
		JOIN users u ON u.id = inserted.user_id AND u.status = 'active'
	`, uuid.New(), parsedUserID, tokenDigest, csrfDigest, expiresAt).Scan(
		&session.ID,
		&session.UserID,
		&session.Username,
		&session.DisplayName,
		&session.Email,
		&session.Locale,
		&session.CSRFDigest,
		&session.CreatedAt,
		&session.ExpiresAt,
		&session.LastSeenAt,
	)
	if err != nil {
		return auth.Session{}, fmt.Errorf("create session: %w", err)
	}
	return session, nil
}

func (store *Store) LookupSession(ctx context.Context, tokenDigest []byte, now time.Time) (auth.Session, error) {
	var session auth.Session
	err := store.pool.QueryRow(ctx, `
		UPDATE sessions s
		SET last_seen_at = $2
		FROM users u
		WHERE s.token_digest = $1
		  AND s.user_id = u.id
		  AND s.revoked_at IS NULL
		  AND s.expires_at > $2
		  AND u.status = 'active'
		RETURNING s.id, s.user_id, u.username, u.display_name, COALESCE(u.email, ''), u.locale,
		          s.csrf_token_digest, s.created_at, s.expires_at, s.last_seen_at
	`, tokenDigest, now).Scan(
		&session.ID,
		&session.UserID,
		&session.Username,
		&session.DisplayName,
		&session.Email,
		&session.Locale,
		&session.CSRFDigest,
		&session.CreatedAt,
		&session.ExpiresAt,
		&session.LastSeenAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.Session{}, auth.ErrInvalidSession
	}
	if err != nil {
		return auth.Session{}, fmt.Errorf("look up session: %w", err)
	}
	return session, nil
}

func (store *Store) RevokeSession(ctx context.Context, tokenDigest []byte, now time.Time) error {
	_, err := store.pool.Exec(ctx, `
		UPDATE sessions
		SET revoked_at = $2
		WHERE token_digest = $1 AND revoked_at IS NULL
	`, tokenDigest, now)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

func (store *Store) CleanupExpiredAuthentication(ctx context.Context, now time.Time) error {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin authentication cleanup: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()

	if _, err := transaction.Exec(ctx, `
		DELETE FROM login_transactions
		WHERE expires_at <= $1 OR (used_at IS NOT NULL AND used_at <= $1 - interval '1 hour')
	`, now); err != nil {
		return fmt.Errorf("clean login transactions: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		DELETE FROM sessions
		WHERE expires_at <= $1 OR (revoked_at IS NOT NULL AND revoked_at <= $1 - interval '1 hour')
	`, now); err != nil {
		return fmt.Errorf("clean sessions: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit authentication cleanup: %w", err)
	}
	return nil
}
