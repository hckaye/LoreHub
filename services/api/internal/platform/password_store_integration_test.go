package platform

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lorehub/lorehub/services/api/internal/auth"
)

func passwordIntegrationSuffix() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
}

func TestPasswordUserLifecycle(t *testing.T) {
	pool, store := identityIntegrationStore(t)
	ctx := context.Background()
	suffix := passwordIntegrationSuffix()
	username := "pw-" + suffix
	email := username + "@example.test"

	user, err := store.CreatePasswordUser(ctx, PasswordUserInput{
		Username:     strings.ToUpper(username),
		Email:        strings.ToUpper(email),
		PasswordHash: "$argon2id$test",
		Locale:       "ja",
	})
	if err != nil {
		t.Fatalf("create password user: %v", err)
	}
	if user.Username != username || user.Email != email || user.Locale != "ja" {
		t.Fatalf("registration input was not normalized: %#v", user)
	}

	if _, err := store.CreatePasswordUser(ctx, PasswordUserInput{
		Username: username, Email: "other-" + email, PasswordHash: "$argon2id$test",
	}); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("duplicate username returned %v", err)
	}
	if _, err := store.CreatePasswordUser(ctx, PasswordUserInput{
		Username: "other-" + username, Email: email, PasswordHash: "$argon2id$test",
	}); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("duplicate email returned %v", err)
	}

	byEmail, err := store.PasswordCredential(ctx, strings.ToUpper(email))
	if err != nil || byEmail.UserID != user.ID {
		t.Fatalf("lookup by email failed: %v %#v", err, byEmail)
	}
	byUsername, err := store.PasswordCredential(ctx, username)
	if err != nil || byUsername.UserID != user.ID {
		t.Fatalf("lookup by username failed: %v %#v", err, byUsername)
	}
	if _, err := store.PasswordCredential(ctx, "missing-"+suffix); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing identifier returned %v", err)
	}

	subject, err := store.ProviderSubject(ctx, user.ID)
	if err != nil || subject != user.ID {
		t.Fatalf("password account has no usable provider subject: %v %q", err, subject)
	}
	var issuer string
	if err := pool.QueryRow(ctx, `
		SELECT issuer FROM user_identities WHERE user_id = $1
	`, user.ID).Scan(&issuer); err != nil || issuer != auth.PasswordIssuer {
		t.Fatalf("password identity issuer is %q (%v)", issuer, err)
	}
}

func TestPasswordFailureCountingAndLocking(t *testing.T) {
	_, store := identityIntegrationStore(t)
	ctx := context.Background()
	suffix := passwordIntegrationSuffix()
	user, err := store.CreatePasswordUser(ctx, PasswordUserInput{
		Username: "lock-" + suffix, Email: "lock-" + suffix + "@example.test", PasswordHash: "$argon2id$test",
	})
	if err != nil {
		t.Fatal(err)
	}
	for expected := 1; expected <= 3; expected++ {
		failures, err := store.RecordPasswordFailure(ctx, user.ID)
		if err != nil || failures != expected {
			t.Fatalf("failure %d recorded as %d (%v)", expected, failures, err)
		}
	}
	until := time.Now().UTC().Add(time.Minute).Truncate(time.Millisecond)
	if err := store.LockPasswordCredential(ctx, user.ID, until); err != nil {
		t.Fatal(err)
	}
	credential, err := store.PasswordCredentialForUser(ctx, user.ID)
	if err != nil || credential.FailedAttempts != 3 || credential.LockedUntil == nil ||
		!credential.LockedUntil.Equal(until) {
		t.Fatalf("lock state was not persisted: %v %#v", err, credential)
	}
	if err := store.SetPassword(ctx, user.ID, "$argon2id$rotated"); err != nil {
		t.Fatal(err)
	}
	credential, err = store.PasswordCredentialForUser(ctx, user.ID)
	if err != nil || credential.PasswordHash != "$argon2id$rotated" || credential.FailedAttempts != 0 ||
		credential.LockedUntil != nil {
		t.Fatalf("password rotation did not clear failure state: %v %#v", err, credential)
	}
	if err := store.SetPassword(ctx, uuid.NewString(), "$argon2id$rotated"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("setting a password for a missing user returned %v", err)
	}
}

func TestPasswordResetTokensAreSingleUseAndReplaced(t *testing.T) {
	_, store := identityIntegrationStore(t)
	ctx := context.Background()
	suffix := passwordIntegrationSuffix()
	user, err := store.CreatePasswordUser(ctx, PasswordUserInput{
		Username: "reset-" + suffix, Email: "reset-" + suffix + "@example.test", PasswordHash: "$argon2id$test",
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := func(value string) []byte {
		sum := sha256.Sum256([]byte(value + suffix))
		return sum[:]
	}
	now := time.Now().UTC()
	if err := store.CreatePasswordReset(ctx, user.ID, digest("first"), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.CreatePasswordReset(ctx, user.ID, digest("second"), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PasswordResetUser(ctx, digest("first"), now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("replaced reset token is still valid: %v", err)
	}
	userID, err := store.PasswordResetUser(ctx, digest("second"), now)
	if err != nil || userID != user.ID {
		t.Fatalf("peeking the reset token failed: %v %q", err, userID)
	}
	if _, err := store.PasswordResetUser(ctx, digest("second"), now.Add(2*time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired reset token is still valid: %v", err)
	}
	consumed, err := store.ConsumePasswordReset(ctx, digest("second"), now)
	if err != nil || consumed != user.ID {
		t.Fatalf("consuming the reset token failed: %v %q", err, consumed)
	}
	if _, err := store.ConsumePasswordReset(ctx, digest("second"), now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reset token was consumable twice: %v", err)
	}
}
