package platform

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lorehub/lorehub/services/api/internal/auth"
)

func TestPersonalAccessTokenLifecycleIntegration(t *testing.T) {
	pool, store := identityIntegrationStore(t)
	ctx := context.Background()
	owner := platformTestUser("pat-owner-" + uuid.NewString()[:8])
	other := platformTestUser("pat-other-" + uuid.NewString()[:8])
	for _, user := range []User{owner, other} {
		mustIdentityExec(t, pool, `
			INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)
		`, user.ID, user.Username, user.DisplayName)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id IN ($1, $2)`, owner.ID, other.ID)
	})
	secrets, err := auth.NewSecretCodec("personal access token integration secret")
	if err != nil {
		t.Fatal(err)
	}
	raw, digest, err := auth.NewPersonalAccessToken(secrets)
	if err != nil {
		t.Fatal(err)
	}
	token, err := store.CreatePersonalAccessToken(ctx, owner, CreatePersonalAccessTokenInput{
		Name:      "Developer workstation",
		Prefix:    auth.PersonalAccessTokenPrefix(raw),
		Digest:    digest,
		Scopes:    []string{auth.ScopeWriteRepository, auth.ScopeReadAPI},
		ExpiresAt: time.Now().UTC().Add(30 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if token.ID == "" || token.Prefix != raw[:12] || token.Name != "Developer workstation" ||
		len(token.Scopes) != 2 {
		t.Fatalf("unexpected token metadata: %+v", token)
	}
	var storedDigest []byte
	var storedPrefix string
	if err := pool.QueryRow(ctx, `
		SELECT token_digest, token_prefix FROM personal_access_tokens WHERE id = $1
	`, token.ID).Scan(&storedDigest, &storedPrefix); err != nil {
		t.Fatal(err)
	}
	if !secrets.Matches(raw, storedDigest) || storedPrefix != raw[:12] {
		t.Fatal("stored personal access token credential does not verify")
	}
	listed, err := store.ListPersonalAccessTokens(ctx, owner)
	if err != nil || len(listed) != 1 || listed[0].ID != token.ID || listed[0].LastUsedAt != nil {
		t.Fatalf("list personal access tokens: tokens=%+v error=%v", listed, err)
	}
	identity, err := store.VerifyPersonalAccessToken(ctx, secrets.Digest(raw), time.Now().UTC())
	if err != nil || identity.UserID != owner.ID || identity.TokenID != token.ID || len(identity.Scopes) != 2 {
		t.Fatalf("verify personal access token: identity=%+v error=%v", identity, err)
	}
	if err := store.ValidatePersonalAccessToken(ctx, token.ID, owner.ID, time.Now().UTC()); err != nil {
		t.Fatalf("validate active personal access token: %v", err)
	}
	listed, err = store.ListPersonalAccessTokens(ctx, owner)
	if err != nil || listed[0].LastUsedAt == nil {
		t.Fatalf("personal access token usage was not recorded: tokens=%+v error=%v", listed, err)
	}
	if err := store.RevokePersonalAccessToken(ctx, other, token.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another user revoked a personal access token: %v", err)
	}
	if err := store.RevokePersonalAccessToken(ctx, owner, token.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.VerifyPersonalAccessToken(
		ctx,
		secrets.Digest(raw),
		time.Now().UTC(),
	); !errors.Is(err, auth.ErrInvalidPersonalAccessToken) {
		t.Fatalf("revoked personal access token error = %v", err)
	}
	if err := store.ValidatePersonalAccessToken(
		ctx,
		token.ID,
		owner.ID,
		time.Now().UTC(),
	); !errors.Is(err, auth.ErrInvalidPersonalAccessToken) {
		t.Fatalf("revoked personal access token state error = %v", err)
	}
	var auditCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM audit_events
		WHERE actor_id = $1 AND target_id = $2
		  AND action IN ('personal_access_token.create', 'personal_access_token.revoke')
	`, owner.ID, token.ID).Scan(&auditCount); err != nil || auditCount != 2 {
		t.Fatalf("personal access token audit count = %d, error=%v", auditCount, err)
	}
}

func TestPersonalAccessTokenRejectsExpiredAndSuspendedCredentialsIntegration(t *testing.T) {
	pool, store := identityIntegrationStore(t)
	ctx := context.Background()
	owner := platformTestUser("pat-state-" + uuid.NewString()[:8])
	mustIdentityExec(t, pool, `
		INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)
	`, owner.ID, owner.Username, owner.DisplayName)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, owner.ID) })
	secrets, _ := auth.NewSecretCodec("personal access token integration secret")
	expiredRaw, expiredDigest, err := auth.NewPersonalAccessToken(secrets)
	if err != nil {
		t.Fatal(err)
	}
	expiredID := uuid.NewString()
	mustIdentityExec(t, pool, `
		INSERT INTO personal_access_tokens (
			id, user_id, name, token_prefix, token_digest, expires_at, created_at
		) VALUES ($1, $2, 'Expired token', $3, $4, now() - interval '1 day', now() - interval '2 days')
	`, expiredID, owner.ID, auth.PersonalAccessTokenPrefix(expiredRaw), expiredDigest)
	mustIdentityExec(t, pool, `
		INSERT INTO personal_access_token_scopes (token_id, scope) VALUES ($1, 'read_api')
	`, expiredID)
	if _, err := store.VerifyPersonalAccessToken(
		ctx,
		secrets.Digest(expiredRaw),
		time.Now().UTC(),
	); !errors.Is(err, auth.ErrInvalidPersonalAccessToken) {
		t.Fatalf("expired personal access token error = %v", err)
	}
	activeRaw, activeDigest, err := auth.NewPersonalAccessToken(secrets)
	if err != nil {
		t.Fatal(err)
	}
	active, err := store.CreatePersonalAccessToken(ctx, owner, CreatePersonalAccessTokenInput{
		Name:      "Suspended owner token",
		Prefix:    auth.PersonalAccessTokenPrefix(activeRaw),
		Digest:    activeDigest,
		Scopes:    []string{auth.ScopeAPI},
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	mustIdentityExec(t, pool, `UPDATE users SET status = 'suspended' WHERE id = $1`, owner.ID)
	if _, err := store.VerifyPersonalAccessToken(
		ctx,
		secrets.Digest(activeRaw),
		time.Now().UTC(),
	); !errors.Is(err, auth.ErrInvalidPersonalAccessToken) {
		t.Fatalf("suspended user personal access token error = %v", err)
	}
	if _, err := store.ListPersonalAccessTokens(ctx, owner); !errors.Is(err, ErrForbidden) {
		t.Fatalf("suspended user listed personal access tokens: %v", err)
	}
	if err := store.RevokePersonalAccessToken(ctx, owner, active.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("suspended user revoked personal access token: %v", err)
	}
}
