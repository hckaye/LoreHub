package platform

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/auth"
)

const (
	personalAccessTokenNameLimit = 80
	personalAccessTokenMaxAge    = 366 * 24 * time.Hour
)

type PersonalAccessToken struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Scopes     []string   `json:"scopes"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	RevokedAt  *time.Time `json:"revokedAt"`
	CreatedAt  time.Time  `json:"createdAt"`
}

type CreatePersonalAccessTokenInput struct {
	Name      string
	Prefix    string
	Digest    []byte
	Scopes    []string
	ExpiresAt time.Time
}

func (store *Store) ListPersonalAccessTokens(
	ctx context.Context,
	actor User,
) ([]PersonalAccessToken, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("begin personal access token list: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	if err := requireActivePersonalAccessTokenActor(ctx, transaction, actor.ID); err != nil {
		return nil, err
	}
	rows, err := transaction.Query(ctx, `
		SELECT token.id, token.name, token.token_prefix, token.expires_at,
		       token.last_used_at, token.revoked_at, token.created_at,
		       array_agg(scope.scope ORDER BY scope.scope)
		FROM personal_access_tokens token
		JOIN personal_access_token_scopes scope ON scope.token_id = token.id
		WHERE token.user_id = $1
		GROUP BY token.id
		ORDER BY token.created_at DESC, token.id DESC
	`, actor.ID)
	if err != nil {
		return nil, fmt.Errorf("list personal access tokens: %w", err)
	}
	defer rows.Close()
	tokens := make([]PersonalAccessToken, 0)
	for rows.Next() {
		var token PersonalAccessToken
		if err := rows.Scan(
			&token.ID,
			&token.Name,
			&token.Prefix,
			&token.ExpiresAt,
			&token.LastUsedAt,
			&token.RevokedAt,
			&token.CreatedAt,
			&token.Scopes,
		); err != nil {
			return nil, fmt.Errorf("scan personal access token: %w", err)
		}
		tokens = append(tokens, token)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate personal access tokens: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit personal access token list: %w", err)
	}
	return tokens, nil
}

func (store *Store) CreatePersonalAccessToken(
	ctx context.Context,
	actor User,
	input CreatePersonalAccessTokenInput,
) (PersonalAccessToken, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Scopes = canonicalPersonalAccessTokenScopes(input.Scopes)
	if !validPersonalAccessTokenInput(input) {
		return PersonalAccessToken{}, ErrInvalidInput
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return PersonalAccessToken{}, fmt.Errorf("begin personal access token creation: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	if err := requireActivePersonalAccessTokenActor(ctx, transaction, actor.ID); err != nil {
		return PersonalAccessToken{}, err
	}
	var createdAt time.Time
	if err := transaction.QueryRow(ctx, "SELECT now()").Scan(&createdAt); err != nil {
		return PersonalAccessToken{}, fmt.Errorf("read personal access token creation time: %w", err)
	}
	if !input.ExpiresAt.After(createdAt) || input.ExpiresAt.After(createdAt.Add(personalAccessTokenMaxAge)) {
		return PersonalAccessToken{}, ErrInvalidInput
	}
	token := PersonalAccessToken{
		ID:        uuid.NewString(),
		Name:      input.Name,
		Prefix:    input.Prefix,
		Scopes:    input.Scopes,
		ExpiresAt: input.ExpiresAt.UTC(),
		CreatedAt: createdAt.UTC(),
	}
	_, err = transaction.Exec(ctx, `
		INSERT INTO personal_access_tokens (
			id, user_id, name, token_prefix, token_digest, expires_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, token.ID, actor.ID, token.Name, token.Prefix, input.Digest, token.ExpiresAt, token.CreatedAt)
	if err != nil {
		return PersonalAccessToken{}, fmt.Errorf("create personal access token: %w", err)
	}
	for _, scope := range token.Scopes {
		if _, err := transaction.Exec(ctx, `
			INSERT INTO personal_access_token_scopes (token_id, scope) VALUES ($1, $2)
		`, token.ID, scope); err != nil {
			return PersonalAccessToken{}, fmt.Errorf("create personal access token scope: %w", err)
		}
	}
	if err := insertAudit(
		ctx,
		transaction,
		actor.ID,
		"",
		"",
		"personal_access_token.create",
		"personal_access_token",
		token.ID,
	); err != nil {
		return PersonalAccessToken{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return PersonalAccessToken{}, fmt.Errorf("commit personal access token creation: %w", err)
	}
	return token, nil
}

func (store *Store) RevokePersonalAccessToken(ctx context.Context, actor User, tokenID string) error {
	if _, err := uuid.Parse(tokenID); err != nil {
		return ErrNotFound
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin personal access token revocation: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	if err := requireActivePersonalAccessTokenActor(ctx, transaction, actor.ID); err != nil {
		return err
	}
	command, err := transaction.Exec(ctx, `
		UPDATE personal_access_tokens
		SET revoked_at = COALESCE(revoked_at, now())
		WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL
	`, tokenID, actor.ID)
	if err != nil {
		return fmt.Errorf("revoke personal access token: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrNotFound
	}
	if err := insertAudit(
		ctx,
		transaction,
		actor.ID,
		"",
		"",
		"personal_access_token.revoke",
		"personal_access_token",
		tokenID,
	); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit personal access token revocation: %w", err)
	}
	return nil
}

func (store *Store) VerifyPersonalAccessToken(
	ctx context.Context,
	digest []byte,
	usedAt time.Time,
) (auth.PersonalAccessTokenIdentity, error) {
	if len(digest) != 32 || usedAt.IsZero() {
		return auth.PersonalAccessTokenIdentity{}, auth.ErrInvalidPersonalAccessToken
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return auth.PersonalAccessTokenIdentity{}, fmt.Errorf("begin personal access token verification: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	var identity auth.PersonalAccessTokenIdentity
	var previousUse *time.Time
	err = transaction.QueryRow(ctx, `
		SELECT token.id, owner.id, owner.username, owner.display_name,
		       COALESCE(owner.email, ''), owner.locale, token.last_used_at,
		       ARRAY(
		           SELECT scope.scope
		           FROM personal_access_token_scopes scope
		           WHERE scope.token_id = token.id
		           ORDER BY scope.scope
		       )
		FROM personal_access_tokens token
		JOIN users owner ON owner.id = token.user_id
		WHERE token.token_digest = $1
		  AND token.revoked_at IS NULL
		  AND token.expires_at > $2
		  AND owner.status = 'active'
		FOR UPDATE OF token
	`, digest, usedAt).Scan(
		&identity.TokenID,
		&identity.UserID,
		&identity.Username,
		&identity.DisplayName,
		&identity.Email,
		&identity.PreferredLocale,
		&previousUse,
		&identity.Scopes,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.PersonalAccessTokenIdentity{}, auth.ErrInvalidPersonalAccessToken
	}
	if err != nil {
		return auth.PersonalAccessTokenIdentity{}, fmt.Errorf("verify personal access token: %w", err)
	}
	if !auth.ValidPersonalAccessTokenScopes(identity.Scopes) {
		return auth.PersonalAccessTokenIdentity{}, auth.ErrInvalidPersonalAccessToken
	}
	if previousUse == nil || previousUse.Before(usedAt.Add(-5*time.Minute)) {
		if _, err := transaction.Exec(ctx, `
			UPDATE personal_access_tokens SET last_used_at = $2 WHERE id = $1
		`, identity.TokenID, usedAt); err != nil {
			return auth.PersonalAccessTokenIdentity{}, fmt.Errorf("record personal access token use: %w", err)
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return auth.PersonalAccessTokenIdentity{}, fmt.Errorf("commit personal access token verification: %w", err)
	}
	return identity, nil
}

func (store *Store) ValidatePersonalAccessToken(
	ctx context.Context,
	tokenID string,
	userID string,
	usedAt time.Time,
) error {
	if _, err := uuid.Parse(tokenID); err != nil {
		return auth.ErrInvalidPersonalAccessToken
	}
	if _, err := uuid.Parse(userID); err != nil || usedAt.IsZero() {
		return auth.ErrInvalidPersonalAccessToken
	}
	var active bool
	err := store.pool.QueryRow(ctx, `
		SELECT true
		FROM personal_access_tokens token
		JOIN users owner ON owner.id = token.user_id
		WHERE token.id = $1
		  AND token.user_id = $2
		  AND token.revoked_at IS NULL
		  AND token.expires_at > $3
		  AND owner.status = 'active'
	`, tokenID, userID, usedAt).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ErrInvalidPersonalAccessToken
	}
	if err != nil {
		return fmt.Errorf("validate personal access token state: %w", err)
	}
	if !active {
		return auth.ErrInvalidPersonalAccessToken
	}
	return nil
}

func requireActivePersonalAccessTokenActor(ctx context.Context, transaction pgx.Tx, actorID string) error {
	if _, err := uuid.Parse(actorID); err != nil {
		return ErrForbidden
	}
	var active bool
	if err := transaction.QueryRow(ctx, `
		SELECT status = 'active' FROM users WHERE id = $1 FOR SHARE
	`, actorID).Scan(&active); errors.Is(err, pgx.ErrNoRows) || !active {
		return ErrForbidden
	} else if err != nil {
		return fmt.Errorf("verify personal access token actor: %w", err)
	}
	return nil
}

func validPersonalAccessTokenInput(input CreatePersonalAccessTokenInput) bool {
	if input.Name == "" || utf8.RuneCountInString(input.Name) > personalAccessTokenNameLimit ||
		len(input.Digest) != 32 || len(input.Prefix) != 12 || !strings.HasPrefix(input.Prefix, "lhp_") ||
		!auth.ValidPersonalAccessTokenScopes(input.Scopes) || input.ExpiresAt.IsZero() {
		return false
	}
	for _, character := range input.Name {
		if unicode.IsControl(character) {
			return false
		}
	}
	for _, character := range input.Prefix[4:] {
		if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '-' || character == '_') {
			return false
		}
	}
	return true
}

func canonicalPersonalAccessTokenScopes(scopes []string) []string {
	result := append([]string(nil), scopes...)
	sort.Strings(result)
	return result
}
