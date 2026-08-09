package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
)

var (
	ErrMissingToken       = errors.New("bearer token is required")
	ErrNotConfigured      = errors.New("OIDC authentication is not configured")
	ErrInvalidTransaction = errors.New("authentication transaction is invalid")
	ErrInvalidSession     = errors.New("session is invalid")
)

const RegistrationPrompt = "create"

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

type LoginProvider interface {
	AuthorizationURL(state string, codeChallenge string, nonce string, prompt string) string
	Exchange(ctx context.Context, code string, codeVerifier string, nonce string) (Principal, error)
}

type OIDCConfig struct {
	Issuer       string
	ClientID     string
	Audience     string
	ClientSecret string
	RedirectURL  string
}

type OIDCAuthenticator struct {
	issuer              string
	accessTokenVerifier *oidc.IDTokenVerifier
}

type DisabledAuthenticator struct{}

type claims struct {
	Subject           string `json:"sub"`
	Issuer            string `json:"iss"`
	Name              string `json:"name"`
	Email             string `json:"email"`
	PreferredUsername string `json:"preferred_username"`
	Locale            string `json:"locale"`
	Nonce             string `json:"nonce"`
}

func NewOIDC(ctx context.Context, issuer string, audience string) (Authenticator, error) {
	if issuer == "" || audience == "" {
		return DisabledAuthenticator{}, nil
	}
	provider, err := newOIDCProvider(ctx, OIDCConfig{Issuer: issuer, ClientID: audience, Audience: audience})
	if err != nil {
		return nil, err
	}
	return provider, nil
}

func (authenticator *OIDCAuthenticator) Authenticate(
	ctx context.Context,
	authorization string,
) (Principal, error) {
	rawToken, err := bearerToken(authorization)
	if err != nil {
		return Principal{}, err
	}
	token, err := authenticator.accessTokenVerifier.Verify(ctx, rawToken)
	if err != nil {
		return Principal{}, fmt.Errorf("verify OIDC token: %w", err)
	}
	var tokenClaims claims
	if err := token.Claims(&tokenClaims); err != nil {
		return Principal{}, fmt.Errorf("read OIDC claims: %w", err)
	}
	return principalFromClaims(authenticator.issuer, tokenClaims, rawToken)
}

func (DisabledAuthenticator) Authenticate(context.Context, string) (Principal, error) {
	return Principal{}, ErrNotConfigured
}

func principalFromClaims(issuer string, tokenClaims claims, accessToken string) (Principal, error) {
	if tokenClaims.Subject == "" {
		return Principal{}, errors.New("OIDC token has no subject")
	}
	if tokenClaims.Issuer != "" {
		issuer = tokenClaims.Issuer
	}
	username := tokenClaims.PreferredUsername
	if username == "" {
		username = tokenClaims.Name
	}
	return Principal{
		Issuer:          issuer,
		Subject:         tokenClaims.Subject,
		Username:        username,
		Name:            tokenClaims.Name,
		Email:           tokenClaims.Email,
		PreferredLocale: tokenClaims.Locale,
		LoreAccessToken: accessToken,
	}, nil
}

func bearerToken(authorization string) (string, error) {
	scheme, token, found := strings.Cut(authorization, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		return "", ErrMissingToken
	}
	return strings.TrimSpace(token), nil
}
