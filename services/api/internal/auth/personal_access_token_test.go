package auth

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type personalAccessTokenVerifierStub struct {
	digest   []byte
	identity PersonalAccessTokenIdentity
	err      error
	stateErr error
}

func (stub *personalAccessTokenVerifierStub) ValidatePersonalAccessToken(
	context.Context,
	string,
	string,
	time.Time,
) error {
	return stub.stateErr
}

func (stub *personalAccessTokenVerifierStub) VerifyPersonalAccessToken(
	_ context.Context,
	digest []byte,
	_ time.Time,
) (PersonalAccessTokenIdentity, error) {
	stub.digest = append([]byte(nil), digest...)
	return stub.identity, stub.err
}

type authenticatorStub struct {
	authorization string
	principal     Principal
	err           error
}

func (stub *authenticatorStub) Authenticate(
	_ context.Context,
	authorization string,
) (Principal, error) {
	stub.authorization = authorization
	return stub.principal, stub.err
}

func TestPersonalAccessTokenAuthenticatorVerifiesTokenAndPreservesScopes(t *testing.T) {
	secrets, err := NewSecretCodec("personal access token test secret")
	if err != nil {
		t.Fatal(err)
	}
	raw, digest, err := NewPersonalAccessToken(secrets)
	if err != nil {
		t.Fatal(err)
	}
	verifier := &personalAccessTokenVerifierStub{identity: PersonalAccessTokenIdentity{
		TokenID:         "token-id",
		UserID:          "user-id",
		Username:        "alice",
		DisplayName:     "Alice",
		Email:           "alice@example.com",
		PreferredLocale: "ja",
		Scopes:          []string{ScopeReadAPI, ScopeWriteRepository},
	}}
	fallback := &authenticatorStub{}
	authenticator, err := NewPersonalAccessTokenAuthenticator(fallback, verifier, secrets)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := authenticator.Authenticate(context.Background(), "Bearer "+raw)
	if err != nil {
		t.Fatal(err)
	}
	if principal.InternalUserID != "user-id" || principal.CredentialKind != CredentialPersonalAccessToken ||
		principal.CredentialID != "token-id" || principal.Username != "alice" ||
		!reflect.DeepEqual(principal.Scopes, verifier.identity.Scopes) {
		t.Fatalf("unexpected principal: %+v", principal)
	}
	if !reflect.DeepEqual(verifier.digest, digest) {
		t.Fatal("personal access token digest did not match the generated token")
	}
	if fallback.authorization != "" {
		t.Fatal("personal access token was sent to the OIDC fallback")
	}
}

func TestPersonalAccessTokenAuthenticatorDelegatesNonPersonalToken(t *testing.T) {
	secrets, _ := NewSecretCodec("personal access token test secret")
	verifier := &personalAccessTokenVerifierStub{}
	fallback := &authenticatorStub{principal: Principal{Subject: "oidc-user"}}
	authenticator, err := NewPersonalAccessTokenAuthenticator(fallback, verifier, secrets)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := authenticator.Authenticate(context.Background(), "Bearer oidc-token")
	if err != nil || principal.Subject != "oidc-user" || fallback.authorization != "Bearer oidc-token" {
		t.Fatalf("OIDC fallback failed: principal=%+v error=%v", principal, err)
	}
	if verifier.digest != nil {
		t.Fatal("OIDC token reached the personal access token verifier")
	}
}

func TestPersonalAccessTokenAuthenticatorRejectsMalformedOrRevokedTokens(t *testing.T) {
	secrets, _ := NewSecretCodec("personal access token test secret")
	verifier := &personalAccessTokenVerifierStub{err: ErrInvalidPersonalAccessToken}
	fallback := &authenticatorStub{}
	authenticator, err := NewPersonalAccessTokenAuthenticator(fallback, verifier, secrets)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"lhp_short", "lhp_abcdefghijklmnopqrstuvwxyz0123456789ABCDE!"} {
		if _, err := authenticator.AuthenticateAPIKey(context.Background(), raw); !errors.Is(
			err,
			ErrInvalidPersonalAccessToken,
		) {
			t.Fatalf("malformed token %q error = %v", raw, err)
		}
	}
	raw, _, err := NewPersonalAccessToken(secrets)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authenticator.AuthenticateAPIKey(context.Background(), raw); !errors.Is(
		err,
		ErrInvalidPersonalAccessToken,
	) {
		t.Fatalf("revoked token error = %v", err)
	}
	if fallback.authorization != "" {
		t.Fatal("invalid personal access token was sent to the OIDC fallback")
	}
}

func TestPersonalAccessTokenAuthenticatorSeparatesVerifierOutages(t *testing.T) {
	secrets, _ := NewSecretCodec("personal access token test secret")
	raw, _, err := NewPersonalAccessToken(secrets)
	if err != nil {
		t.Fatal(err)
	}
	verifier := &personalAccessTokenVerifierStub{err: errors.New("database unavailable")}
	authenticator, err := NewPersonalAccessTokenAuthenticator(&authenticatorStub{}, verifier, secrets)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authenticator.AuthenticateAPIKey(context.Background(), raw); !errors.Is(
		err,
		ErrAuthenticationUnavailable,
	) {
		t.Fatalf("verifier outage error = %v", err)
	}
	verifier.stateErr = errors.New("database unavailable")
	if err := authenticator.ValidateAPIKeyCredential(
		context.Background(),
		"token-id",
		"user-id",
	); !errors.Is(err, ErrAuthenticationUnavailable) {
		t.Fatalf("state verifier outage error = %v", err)
	}
}

func TestPersonalAccessTokenScopeValidationAndAuthorization(t *testing.T) {
	if ValidPersonalAccessTokenScopes(nil) ||
		ValidPersonalAccessTokenScopes([]string{ScopeReadAPI, ScopeReadAPI}) ||
		ValidPersonalAccessTokenScopes([]string{"unknown"}) {
		t.Fatal("invalid personal access token scopes were accepted")
	}
	if !ValidPersonalAccessTokenScopes([]string{ScopeReadAPI, ScopeWriteRepository}) {
		t.Fatal("valid personal access token scopes were rejected")
	}
	if !PersonalAccessTokenAllowsAPI([]string{ScopeReadAPI}, false) ||
		PersonalAccessTokenAllowsAPI([]string{ScopeReadAPI}, true) ||
		!PersonalAccessTokenAllowsAPI([]string{ScopeAPI}, true) {
		t.Fatal("API scope authorization is incorrect")
	}
	if !PersonalAccessTokenAllowsRepository([]string{ScopeReadRepository}) ||
		PersonalAccessTokenAllowsRepository([]string{ScopeAPI}) {
		t.Fatal("repository scope authorization is incorrect")
	}
}
