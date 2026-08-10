package runner

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresExecutionContextResolverRequiresAES256Key(t *testing.T) {
	pool := new(pgxpool.Pool)
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x5a}, 32))
	if _, err := NewPostgresExecutionContextResolver(pool, "actions-key-2026-08", key); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		keyID string
		key   string
	}{
		{name: "missing key ID", keyID: "", key: key},
		{name: "unsafe key ID", keyID: "key/id", key: key},
		{name: "invalid base64", keyID: "key-1", key: "not-base64"},
		{name: "internal base64 newline", keyID: "key-1", key: key[:8] + "\n" + key[8:]},
		{
			name: "wrong key length", keyID: "key-1",
			key: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x5a}, 31)),
		},
		{name: "base64 whitespace", keyID: "key-1", key: key + "\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewPostgresExecutionContextResolver(pool, test.keyID, test.key); err == nil {
				t.Fatal("invalid encryption configuration was accepted")
			}
		})
	}
	if _, err := NewPostgresExecutionContextResolver(nil, "key-1", key); err == nil {
		t.Fatal("nil PostgreSQL pool was accepted")
	}
}

func TestActionsSecretEncryptionAuthenticatesItsScopeAndName(t *testing.T) {
	resolver := newTestExecutionContextResolver(t, new(pgxpool.Pool))
	organizationID := uuid.NewString()
	repositoryID := uuid.NewString()
	nonceA := bytes.Repeat([]byte{0x11}, resolver.aead.NonceSize())
	nonceB := bytes.Repeat([]byte{0x22}, resolver.aead.NonceSize())
	aad := executionContextAAD(
		executionContextScopeEnvironment, organizationID, repositoryID, "production", "DEPLOY_TOKEN",
	)
	ciphertextA := resolver.aead.Seal(nil, nonceA, []byte("sensitive-value"), aad)
	ciphertextB := resolver.aead.Seal(nil, nonceB, []byte("sensitive-value"), aad)
	if bytes.Equal(ciphertextA, ciphertextB) {
		t.Fatal("different nonces produced identical secret ciphertext")
	}
	plaintext, err := resolver.aead.Open(nil, nonceA, ciphertextA, aad)
	if err != nil || string(plaintext) != "sensitive-value" {
		t.Fatalf("secret did not decrypt with its exact AAD: %q, %v", plaintext, err)
	}
	for _, changedAAD := range [][]byte{
		executionContextAAD(
			executionContextScopeRepository, organizationID, repositoryID, "", "DEPLOY_TOKEN",
		),
		executionContextAAD(
			executionContextScopeEnvironment, organizationID, uuid.NewString(), "production", "DEPLOY_TOKEN",
		),
		executionContextAAD(
			executionContextScopeEnvironment, organizationID, repositoryID, "production", "OTHER_TOKEN",
		),
	} {
		if _, err := resolver.aead.Open(nil, nonceA, ciphertextA, changedAAD); err == nil {
			t.Fatal("secret decrypted after its authenticated scope or name changed")
		}
	}
}

func TestExecutionContextManagementScopeValidation(t *testing.T) {
	organizationID := uuid.NewString()
	repositoryID := uuid.NewString()
	valid := []ExecutionContextScope{
		{Kind: executionContextScopeOrganization, OrganizationID: organizationID},
		{
			Kind: executionContextScopeRepository, OrganizationID: organizationID,
			RepositoryID: repositoryID,
		},
		{
			Kind: executionContextScopeEnvironment, OrganizationID: organizationID,
			RepositoryID: repositoryID, Environment: "production",
		},
	}
	for _, scope := range valid {
		if _, err := validateExecutionContextScope(context.Background(), scope); err != nil {
			t.Fatalf("valid scope was rejected: %#v: %v", scope, err)
		}
	}
	invalid := []ExecutionContextScope{
		{Kind: "unknown", OrganizationID: organizationID},
		{Kind: executionContextScopeOrganization, OrganizationID: organizationID, RepositoryID: repositoryID},
		{Kind: executionContextScopeRepository, OrganizationID: organizationID},
		{
			Kind: executionContextScopeEnvironment, OrganizationID: organizationID,
			RepositoryID: repositoryID,
		},
		{
			Kind: executionContextScopeEnvironment, OrganizationID: organizationID,
			RepositoryID: repositoryID, Environment: " production",
		},
	}
	for _, scope := range invalid {
		if _, err := validateExecutionContextScope(context.Background(), scope); err == nil {
			t.Fatalf("invalid scope was accepted: %#v", scope)
		}
	}
	if err := validateExecutionContextEntryName("GITHUB_TOKEN"); err == nil {
		t.Fatal("reserved Actions name was accepted")
	}
	if err := validateExecutionContextEntryName(strings.Repeat("A", 101)); err == nil {
		t.Fatal("oversized Actions name was accepted")
	}
}

func TestExecutionContextErrorClassification(t *testing.T) {
	organizationID := uuid.NewString()
	repositoryID := uuid.NewString()
	scope := ExecutionContextScope{
		Kind: executionContextScopeRepository, OrganizationID: organizationID,
		RepositoryID: repositoryID,
	}
	invalidInputs := []struct {
		name      string
		scope     ExecutionContextScope
		entryName string
		value     string
		actorID   string
	}{
		{
			name: "scope", scope: ExecutionContextScope{Kind: "invalid", OrganizationID: organizationID},
			entryName: "VALID", actorID: uuid.NewString(),
		},
		{name: "name", scope: scope, entryName: "GITHUB_TOKEN", actorID: uuid.NewString()},
		{
			name: "value", scope: scope, entryName: "VALID",
			value: strings.Repeat("x", (1<<20)+1), actorID: uuid.NewString(),
		},
		{name: "actor", scope: scope, entryName: "VALID", actorID: "not-a-uuid"},
	}
	for _, test := range invalidInputs {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := validateExecutionContextMutation(
				context.Background(), test.scope, test.entryName, test.value, test.actorID, false,
			)
			if !errors.Is(err, ErrExecutionContextInvalid) {
				t.Fatalf("invalid input was not classified: %v", err)
			}
			if errors.Is(err, ErrExecutionContextUnauthorized) || errors.Is(err, ErrExecutionContextEntryNotFound) {
				t.Fatalf("invalid input used the wrong error class: %v", err)
			}
		})
	}
	request := ExecutionContextRequest{
		Principal:      CredentialPrincipal{Kind: "service", Subject: "runner"},
		RepositoryID:   "not-a-uuid",
		OrganizationID: organizationID,
		RequestedScope: "actions:execute",
	}
	_, err := validateProductionExecutionContextRequest(context.Background(), request)
	if !errors.Is(err, ErrExecutionContextInvalid) {
		t.Fatalf("invalid production resolver request was not classified: %v", err)
	}
	if errors.Is(ErrExecutionContextUnauthorized, ErrExecutionContextInvalid) ||
		errors.Is(ErrExecutionContextEntryNotFound, ErrExecutionContextInvalid) {
		t.Fatal("execution context error sentinels overlap")
	}
	closedPool, err := pgxpool.New(context.Background(), "postgresql://localhost/lorehub")
	if err != nil {
		t.Fatal(err)
	}
	closedPool.Close()
	resolver := newTestExecutionContextResolver(t, closedPool)
	_, err = resolver.ListExecutionContextEntries(context.Background(), uuid.NewString(), scope)
	if err == nil || errors.Is(err, ErrExecutionContextInvalid) ||
		errors.Is(err, ErrExecutionContextUnauthorized) || errors.Is(err, ErrExecutionContextEntryNotFound) {
		t.Fatalf("database failure was not kept as an internal error: %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := validateExecutionContextMutation(
		cancelled, scope, "VALID", "value", uuid.NewString(), false,
	); !errors.Is(err, context.Canceled) || errors.Is(err, ErrExecutionContextInvalid) {
		t.Fatalf("context cancellation was misclassified: %v", err)
	}
}

func newTestExecutionContextResolver(
	t *testing.T,
	pool *pgxpool.Pool,
) *PostgresExecutionContextResolver {
	t.Helper()
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x42}, 32))
	resolver, err := NewPostgresExecutionContextResolver(pool, "actions-test-key", key)
	if err != nil {
		t.Fatal(err)
	}
	return resolver
}
