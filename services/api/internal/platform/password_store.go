package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lorehub/lorehub/services/api/internal/auth"
)

var (
	ErrUsernameTaken = errors.New("the username is already taken")
	ErrEmailTaken    = errors.New("the email address is already registered")
)

type PasswordUserInput struct {
	Username     string
	Email        string
	PasswordHash string
	Locale       string
}

type PasswordCredential struct {
	UserID         string
	Email          string
	PasswordHash   string
	FailedAttempts int
	LockedUntil    *time.Time
}

func (store *Store) CreatePasswordUser(ctx context.Context, input PasswordUserInput) (User, error) {
	username := strings.ToLower(strings.TrimSpace(input.Username))
	email := strings.ToLower(strings.TrimSpace(input.Email))
	if !slugPattern.MatchString(username) || email == "" || input.PasswordHash == "" {
		return User{}, ErrInvalidInput
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return User{}, fmt.Errorf("begin password registration: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()

	userID := uuid.New()
	locale := normalizedLocale(input.Locale)
	_, err = transaction.Exec(ctx, `
		INSERT INTO users (id, username, display_name, email, locale)
		VALUES ($1, $2, $3, $4, $5)
	`, userID, username, limitText(username, 160), limitText(email, 320), locale)
	if err != nil {
		return User{}, translatePasswordConstraintError("create password user", err)
	}
	_, err = transaction.Exec(ctx, `
		INSERT INTO user_identities (id, user_id, issuer, subject)
		VALUES ($1, $2, $3, $4)
	`, uuid.New(), userID, auth.PasswordIssuer, userID.String())
	if err != nil {
		return User{}, fmt.Errorf("create password identity: %w", err)
	}
	_, err = transaction.Exec(ctx, `
		INSERT INTO user_passwords (user_id, email, password_hash)
		VALUES ($1, $2, $3)
	`, userID, limitText(email, 320), input.PasswordHash)
	if err != nil {
		return User{}, translatePasswordConstraintError("create password credential", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return User{}, fmt.Errorf("commit password registration: %w", err)
	}
	return User{
		ID:          userID.String(),
		Username:    username,
		DisplayName: username,
		Email:       email,
		Locale:      locale,
	}, nil
}

func (store *Store) PasswordCredential(ctx context.Context, identifier string) (PasswordCredential, error) {
	identifier = strings.ToLower(strings.TrimSpace(identifier))
	if identifier == "" {
		return PasswordCredential{}, ErrNotFound
	}
	var credential PasswordCredential
	err := store.pool.QueryRow(ctx, `
		SELECT p.user_id, p.email, p.password_hash, p.failed_attempts, p.locked_until
		FROM user_passwords p
		JOIN users u ON u.id = p.user_id
		WHERE (p.email = $1 OR u.username = $1) AND u.status = 'active'
	`, identifier).Scan(
		&credential.UserID,
		&credential.Email,
		&credential.PasswordHash,
		&credential.FailedAttempts,
		&credential.LockedUntil,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return PasswordCredential{}, ErrNotFound
	}
	if err != nil {
		return PasswordCredential{}, fmt.Errorf("find password credential: %w", err)
	}
	return credential, nil
}

func (store *Store) PasswordCredentialForUser(ctx context.Context, userID string) (PasswordCredential, error) {
	var credential PasswordCredential
	err := store.pool.QueryRow(ctx, `
		SELECT p.user_id, p.email, p.password_hash, p.failed_attempts, p.locked_until
		FROM user_passwords p
		JOIN users u ON u.id = p.user_id
		WHERE p.user_id = $1 AND u.status = 'active'
	`, userID).Scan(
		&credential.UserID,
		&credential.Email,
		&credential.PasswordHash,
		&credential.FailedAttempts,
		&credential.LockedUntil,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return PasswordCredential{}, ErrNotFound
	}
	if err != nil {
		return PasswordCredential{}, fmt.Errorf("find password credential: %w", err)
	}
	return credential, nil
}

func (store *Store) RecordPasswordFailure(ctx context.Context, userID string) (int, error) {
	var failures int
	err := store.pool.QueryRow(ctx, `
		UPDATE user_passwords
		SET failed_attempts = failed_attempts + 1, updated_at = now()
		WHERE user_id = $1
		RETURNING failed_attempts
	`, userID).Scan(&failures)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("record password failure: %w", err)
	}
	return failures, nil
}

func (store *Store) LockPasswordCredential(ctx context.Context, userID string, until time.Time) error {
	if _, err := store.pool.Exec(ctx, `
		UPDATE user_passwords
		SET locked_until = $2, updated_at = now()
		WHERE user_id = $1
	`, userID, until); err != nil {
		return fmt.Errorf("lock password credential: %w", err)
	}
	return nil
}

func (store *Store) ClearPasswordFailures(ctx context.Context, userID string) error {
	if _, err := store.pool.Exec(ctx, `
		UPDATE user_passwords
		SET failed_attempts = 0, locked_until = NULL, updated_at = now()
		WHERE user_id = $1
	`, userID); err != nil {
		return fmt.Errorf("clear password failures: %w", err)
	}
	return nil
}

func (store *Store) SetPassword(ctx context.Context, userID string, passwordHash string) error {
	if passwordHash == "" {
		return ErrInvalidInput
	}
	result, err := store.pool.Exec(ctx, `
		UPDATE user_passwords
		SET password_hash = $2, failed_attempts = 0, locked_until = NULL, updated_at = now()
		WHERE user_id = $1
	`, userID, passwordHash)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (store *Store) RevokeOtherSessions(
	ctx context.Context,
	userID string,
	keepTokenDigest []byte,
	now time.Time,
) error {
	if _, err := store.pool.Exec(ctx, `
		UPDATE sessions
		SET revoked_at = $3
		WHERE user_id = $1 AND revoked_at IS NULL AND token_digest <> $2
	`, userID, keepTokenDigest, now); err != nil {
		return fmt.Errorf("revoke other sessions: %w", err)
	}
	return nil
}

func translatePasswordConstraintError(operation string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		switch pgErr.ConstraintName {
		case "users_username_unique":
			return ErrUsernameTaken
		case "user_passwords_email_unique":
			return ErrEmailTaken
		}
		return fmt.Errorf("%s: %w", operation, ErrConflict)
	}
	return fmt.Errorf("%s: %w", operation, err)
}
