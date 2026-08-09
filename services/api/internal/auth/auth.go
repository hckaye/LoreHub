package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
)

var (
	ErrMissingToken  = errors.New("bearer token is required")
	ErrNotConfigured = errors.New("OIDC authentication is not configured")
)

type Principal struct {
	Issuer          string
	Subject         string
	Username        string
	Name            string
	Email           string
	PreferredLocale string
	LoreAccessToken string
}

type Authenticator interface {
	Authenticate(ctx context.Context, authorization string) (Principal, error)
}

type OIDCAuthenticator struct {
	issuer   string
	verifier *oidc.IDTokenVerifier
}

type DisabledAuthenticator struct{}

type claims struct {
	Subject           string `json:"sub"`
	Name              string `json:"name"`
	Email             string `json:"email"`
	PreferredUsername string `json:"preferred_username"`
	Locale            string `json:"locale"`
}

func NewOIDC(ctx context.Context, issuer string, audience string) (Authenticator, error) {
	if issuer == "" || audience == "" {
		return DisabledAuthenticator{}, nil
	}
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC provider: %w", err)
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: audience})
	return &OIDCAuthenticator{issuer: issuer, verifier: verifier}, nil
}

func (authenticator *OIDCAuthenticator) Authenticate(
	ctx context.Context,
	authorization string,
) (Principal, error) {
	rawToken, err := bearerToken(authorization)
	if err != nil {
		return Principal{}, err
	}
	token, err := authenticator.verifier.Verify(ctx, rawToken)
	if err != nil {
		return Principal{}, fmt.Errorf("verify OIDC token: %w", err)
	}
	var tokenClaims claims
	if err := token.Claims(&tokenClaims); err != nil {
		return Principal{}, fmt.Errorf("read OIDC claims: %w", err)
	}
	if tokenClaims.Subject == "" {
		return Principal{}, errors.New("OIDC token has no subject")
	}
	username := tokenClaims.PreferredUsername
	if username == "" {
		username = tokenClaims.Name
	}
	return Principal{
		Issuer:          authenticator.issuer,
		Subject:         tokenClaims.Subject,
		Username:        username,
		Name:            tokenClaims.Name,
		Email:           tokenClaims.Email,
		PreferredLocale: tokenClaims.Locale,
		LoreAccessToken: rawToken,
	}, nil
}

func (DisabledAuthenticator) Authenticate(context.Context, string) (Principal, error) {
	return Principal{}, ErrNotConfigured
}

func bearerToken(authorization string) (string, error) {
	scheme, token, found := strings.Cut(authorization, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		return "", ErrMissingToken
	}
	return strings.TrimSpace(token), nil
}
