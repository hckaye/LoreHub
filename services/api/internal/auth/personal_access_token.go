package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	CredentialPersonalAccessToken = "personal_access_token"
	ScopeReadAPI                  = "read_api"
	ScopeAPI                      = "api"
	ScopeReadRepository           = "read_repository"
	ScopeWriteRepository          = "write_repository"
	personalAccessTokenPrefix     = "lhp_"
)

var (
	ErrInvalidPersonalAccessToken = errors.New("personal access token is invalid")
	ErrAuthenticationUnavailable  = errors.New("authentication is unavailable")
)

var personalAccessTokenScopes = map[string]bool{
	ScopeReadAPI:         true,
	ScopeAPI:             true,
	ScopeReadRepository:  true,
	ScopeWriteRepository: true,
}

type PersonalAccessTokenIdentity struct {
	TokenID         string
	UserID          string
	Username        string
	DisplayName     string
	Email           string
	PreferredLocale string
	Scopes          []string
}

type PersonalAccessTokenVerifier interface {
	VerifyPersonalAccessToken(
		ctx context.Context,
		digest []byte,
		usedAt time.Time,
	) (PersonalAccessTokenIdentity, error)
	ValidatePersonalAccessToken(
		ctx context.Context,
		tokenID string,
		userID string,
		usedAt time.Time,
	) error
}

type PersonalAccessTokenAuthenticator struct {
	fallback Authenticator
	verifier PersonalAccessTokenVerifier
	secrets  *SecretCodec
}

func NewPersonalAccessTokenAuthenticator(
	fallback Authenticator,
	verifier PersonalAccessTokenVerifier,
	secrets *SecretCodec,
) (*PersonalAccessTokenAuthenticator, error) {
	if fallback == nil || verifier == nil || secrets == nil {
		return nil, errors.New("personal access token authentication dependencies are required")
	}
	return &PersonalAccessTokenAuthenticator{fallback: fallback, verifier: verifier, secrets: secrets}, nil
}

func (authenticator *PersonalAccessTokenAuthenticator) Authenticate(
	ctx context.Context,
	authorization string,
) (Principal, error) {
	raw, err := bearerToken(authorization)
	if err != nil {
		return Principal{}, err
	}
	if !strings.HasPrefix(raw, personalAccessTokenPrefix) {
		return authenticator.fallback.Authenticate(ctx, authorization)
	}
	return authenticator.AuthenticateAPIKey(ctx, raw)
}

func (authenticator *PersonalAccessTokenAuthenticator) AuthenticateAPIKey(
	ctx context.Context,
	raw string,
) (Principal, error) {
	if !ValidPersonalAccessToken(raw) {
		return Principal{}, ErrInvalidPersonalAccessToken
	}
	identity, err := authenticator.verifier.VerifyPersonalAccessToken(
		ctx,
		authenticator.secrets.Digest(raw),
		time.Now().UTC(),
	)
	if err != nil {
		if errors.Is(err, ErrInvalidPersonalAccessToken) {
			return Principal{}, ErrInvalidPersonalAccessToken
		}
		return Principal{}, fmt.Errorf("%w: verify personal access token", ErrAuthenticationUnavailable)
	}
	if identity.UserID == "" || identity.TokenID == "" || !ValidPersonalAccessTokenScopes(identity.Scopes) {
		return Principal{}, ErrInvalidPersonalAccessToken
	}
	return Principal{
		InternalUserID:  identity.UserID,
		Subject:         identity.UserID,
		Username:        identity.Username,
		Name:            identity.DisplayName,
		Email:           identity.Email,
		PreferredLocale: identity.PreferredLocale,
		CredentialKind:  CredentialPersonalAccessToken,
		CredentialID:    identity.TokenID,
		Scopes:          append([]string(nil), identity.Scopes...),
	}, nil
}

func (authenticator *PersonalAccessTokenAuthenticator) ValidateAPIKeyCredential(
	ctx context.Context,
	credentialID string,
	userID string,
) error {
	if credentialID == "" || userID == "" {
		return ErrInvalidPersonalAccessToken
	}
	err := authenticator.verifier.ValidatePersonalAccessToken(
		ctx,
		credentialID,
		userID,
		time.Now().UTC(),
	)
	if err == nil || errors.Is(err, ErrInvalidPersonalAccessToken) {
		return err
	}
	return fmt.Errorf("%w: validate personal access token", ErrAuthenticationUnavailable)
}

func NewPersonalAccessToken(secrets *SecretCodec) (string, []byte, error) {
	if secrets == nil {
		return "", nil, errors.New("personal access token secret codec is required")
	}
	random, err := randomToken()
	if err != nil {
		return "", nil, err
	}
	raw := personalAccessTokenPrefix + random
	return raw, secrets.Digest(raw), nil
}

func ValidPersonalAccessToken(raw string) bool {
	if len(raw) != len(personalAccessTokenPrefix)+43 || !strings.HasPrefix(raw, personalAccessTokenPrefix) {
		return false
	}
	for _, character := range raw[len(personalAccessTokenPrefix):] {
		if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '-' || character == '_') {
			return false
		}
	}
	return true
}

func PersonalAccessTokenPrefix(raw string) string {
	if !ValidPersonalAccessToken(raw) {
		return ""
	}
	return raw[:12]
}

func ValidPersonalAccessTokenScopes(scopes []string) bool {
	if len(scopes) == 0 || len(scopes) > len(personalAccessTokenScopes) {
		return false
	}
	seen := make(map[string]bool, len(scopes))
	for _, scope := range scopes {
		if !personalAccessTokenScopes[scope] || seen[scope] {
			return false
		}
		seen[scope] = true
	}
	return true
}

func PersonalAccessTokenAllowsAPI(scopes []string, stateChanging bool) bool {
	available := scopeSet(scopes)
	if available[ScopeAPI] {
		return true
	}
	return !stateChanging && available[ScopeReadAPI]
}

func PersonalAccessTokenAllowsRepository(scopes []string) bool {
	available := scopeSet(scopes)
	return available[ScopeReadRepository] || available[ScopeWriteRepository]
}

func scopeSet(scopes []string) map[string]bool {
	result := make(map[string]bool, len(scopes))
	for _, scope := range scopes {
		result[scope] = true
	}
	return result
}
