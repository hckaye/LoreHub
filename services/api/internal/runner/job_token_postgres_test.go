package runner

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type staticJobTokenKeys struct {
	current jose.JSONWebKey
	public  []jose.JSONWebKey
}

func (keys staticJobTokenKeys) Current() jose.JSONWebKey {
	return keys.current
}

func (keys staticJobTokenKeys) PublicKeys() []jose.JSONWebKey {
	return append([]jose.JSONWebKey(nil), keys.public...)
}

type jobTokenTestDatabase struct {
	allowed bool
	err     error
	calls   int
}

func (database *jobTokenTestDatabase) QueryRow(context.Context, string, ...any) pgx.Row {
	database.calls++
	return jobTokenTestRow{allowed: database.allowed, err: database.err}
}

type jobTokenTestRow struct {
	allowed bool
	err     error
}

func (row jobTokenTestRow) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(destinations) != 1 {
		return errors.New("unexpected job token test scan")
	}
	allowed, ok := destinations[0].(*bool)
	if !ok {
		return errors.New("unexpected job token test destination")
	}
	*allowed = row.allowed
	return nil
}

func TestPostgresJobTokenIssueAndVerifyExactClaims(t *testing.T) {
	database := &jobTokenTestDatabase{allowed: true}
	service, err := newPostgresJobTokenService(
		database,
		newJobTokenTestKeys(t, "actions-current"),
		"https://lorehub.example/actions",
		"lorehub-actions",
	)
	if err != nil {
		t.Fatal(err)
	}
	request := validPostgresJobTokenRequest()
	issued, err := service.Issue(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if issued.RepositoryID != request.RepositoryID || issued.Subject != request.ServicePrincipal.Subject ||
		issued.Token == "" || issued.ExpiresAt.After(request.RequestedExpiry) {
		t.Fatalf("unexpected issued token metadata: %#v", issued)
	}
	verified, err := service.Verify(context.Background(), issued.Token, request.RESTScope, request.GraphQLScope)
	if err != nil {
		t.Fatal(err)
	}
	claims := verified.Claims
	if claims.JobID != request.JobID || claims.RunID != request.RunID || claims.Attempt != request.Attempt ||
		claims.RepositoryID != request.RepositoryID || claims.ActorID != request.ActorID ||
		claims.Subject != request.ServicePrincipal.Subject || claims.PrincipalKind != "service" ||
		claims.RESTScope != request.RESTScope || claims.GraphQLScope != request.GraphQLScope {
		t.Fatalf("verified claims did not retain the exact request: %#v", claims)
	}
	if len(claims.Audience) != 1 || claims.Audience[0] != "lorehub-actions" || database.calls != 2 {
		t.Fatalf("unexpected audience or authorization count: %#v calls=%d", claims.Audience, database.calls)
	}
}

func TestPostgresJobTokenRejectsScopeTamperingAndRevokedState(t *testing.T) {
	database := &jobTokenTestDatabase{allowed: true}
	service, err := newPostgresJobTokenService(
		database,
		newJobTokenTestKeys(t, "actions-current"),
		"https://lorehub.example/actions",
		"lorehub-actions",
	)
	if err != nil {
		t.Fatal(err)
	}
	request := validPostgresJobTokenRequest()
	issued, err := service.Issue(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Verify(
		context.Background(), issued.Token, request.RESTScope+":write", request.GraphQLScope,
	); !errors.Is(err, ErrActionsJobTokenScope) {
		t.Fatalf("mismatched REST scope returned %v", err)
	}
	if _, err := service.Verify(
		context.Background(), issued.Token+"x", request.RESTScope, request.GraphQLScope,
	); !errors.Is(err, ErrActionsJobTokenInvalid) {
		t.Fatalf("tampered token returned %v", err)
	}
	database.allowed = false
	if _, err := service.Verify(
		context.Background(), issued.Token, request.RESTScope, request.GraphQLScope,
	); !errors.Is(err, ErrActionsJobTokenUnauthorized) {
		t.Fatalf("revoked database state returned %v", err)
	}
}

func TestPostgresJobTokenRejectsInvalidConstructionAndRequests(t *testing.T) {
	keys := newJobTokenTestKeys(t, "actions-current")
	database := &jobTokenTestDatabase{allowed: true}
	if _, err := newPostgresJobTokenService(database, keys, " issuer ", "audience"); err == nil {
		t.Fatal("issuer whitespace was accepted")
	}
	badKeys := keys
	badKeys.current.Algorithm = string(jose.ES256)
	if _, err := newPostgresJobTokenService(database, badKeys, "issuer", "audience"); err == nil {
		t.Fatal("non-RS256 current key was accepted")
	}
	service, err := newPostgresJobTokenService(database, keys, "issuer", "audience")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*JobTokenRequest)
	}{
		{name: "principal kind", mutate: func(request *JobTokenRequest) {
			request.ServicePrincipal.Kind = "user"
		}},
		{name: "job id", mutate: func(request *JobTokenRequest) { request.JobID = "not-a-uuid" }},
		{name: "actor id", mutate: func(request *JobTokenRequest) { request.ActorID = "not-a-uuid" }},
		{name: "scope", mutate: func(request *JobTokenRequest) { request.RESTScope = "actions job" }},
		{name: "expiry", mutate: func(request *JobTokenRequest) {
			request.RequestedExpiry = time.Now().UTC().Add(16 * time.Minute)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := validPostgresJobTokenRequest()
			test.mutate(&request)
			if _, err := service.Issue(context.Background(), request); err == nil {
				t.Fatal("invalid job token request was accepted")
			}
		})
	}
}

func validPostgresJobTokenRequest() JobTokenRequest {
	return JobTokenRequest{
		JobID:        uuid.NewString(),
		RunID:        uuid.NewString(),
		Attempt:      2,
		RepositoryID: uuid.NewString(),
		ActorID:      uuid.NewString(),
		ServicePrincipal: CredentialPrincipal{
			Kind: "service", Subject: "00000000-0000-4000-8000-000000000002",
		},
		RESTScope:       "actions:job:rest",
		GraphQLScope:    "actions:job:graphql",
		RequestedExpiry: time.Now().UTC().Add(10 * time.Minute),
	}
}

func newJobTokenTestKeys(t *testing.T, keyID string) staticJobTokenKeys {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	current := jose.JSONWebKey{
		Key: privateKey, KeyID: keyID, Use: "sig", Algorithm: string(jose.RS256),
	}
	return staticJobTokenKeys{current: current, public: []jose.JSONWebKey{current.Public()}}
}

func TestJobTokenErrorsDoNotContainTokenMaterial(t *testing.T) {
	database := &jobTokenTestDatabase{allowed: false}
	service, err := newPostgresJobTokenService(
		database,
		newJobTokenTestKeys(t, "actions-current"),
		"issuer",
		"audience",
	)
	if err != nil {
		t.Fatal(err)
	}
	secret := strings.Repeat("secret-token-material", 4)
	_, err = service.Verify(context.Background(), secret, "actions:job", "actions:job")
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("verification error exposed token material: %v", err)
	}
}
