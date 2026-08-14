package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type OIDCProvider struct {
	OIDCAuthenticator
	idTokenVerifier *oidc.IDTokenVerifier
	oauthConfig     oauth2.Config
	oidcProvider    *oidc.Provider
}

func NewOIDCProvider(ctx context.Context, config OIDCConfig) (*OIDCProvider, error) {
	return newOIDCProvider(ctx, config)
}

func newOIDCProvider(ctx context.Context, config OIDCConfig) (*OIDCProvider, error) {
	if config.Issuer == "" || config.ClientID == "" || config.Audience == "" {
		return nil, errors.New("OIDC issuer, client ID, and audience are required")
	}
	provider, err := oidc.NewProvider(ctx, config.Issuer)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC provider: %w", err)
	}
	idTokenVerifier := provider.Verifier(&oidc.Config{ClientID: config.ClientID})
	accessTokenVerifier := provider.Verifier(&oidc.Config{ClientID: config.Audience})
	return &OIDCProvider{
		OIDCAuthenticator: OIDCAuthenticator{
			issuer:              config.Issuer,
			accessTokenVerifier: accessTokenVerifier,
		},
		idTokenVerifier: idTokenVerifier,
		oidcProvider:    provider,
		oauthConfig: oauth2.Config{
			ClientID:     config.ClientID,
			ClientSecret: config.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  config.RedirectURL,
			Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		},
	}, nil
}

func (provider *OIDCProvider) AuthorizationURL(
	state string,
	codeChallenge string,
	nonce string,
	prompt string,
) string {
	return provider.AuthorizationURLForProvider(state, codeChallenge, nonce, prompt, "")
}

func (provider *OIDCProvider) AuthorizationURLForProvider(
	state string,
	codeChallenge string,
	nonce string,
	prompt string,
	providerHint string,
) string {
	options := []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("nonce", nonce),
	}
	if providerHint != "" {
		options = append(options, oauth2.SetAuthURLParam("kc_idp_hint", providerHint))
	}
	if prompt == RegistrationPrompt {
		options = append(options, oauth2.SetAuthURLParam("prompt", RegistrationPrompt))
	}
	return provider.oauthConfig.AuthCodeURL(state, options...)
}

func (provider *OIDCProvider) Exchange(
	ctx context.Context,
	code string,
	codeVerifier string,
	nonce string,
) (Principal, error) {
	token, err := provider.oauthConfig.Exchange(
		ctx,
		code,
		oauth2.SetAuthURLParam("code_verifier", codeVerifier),
	)
	if err != nil {
		return Principal{}, fmt.Errorf("exchange OIDC authorization code: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return Principal{}, errors.New("OIDC token response has no ID token")
	}
	idToken, err := provider.idTokenVerifier.Verify(ctx, rawIDToken)
	if err != nil {
		return Principal{}, fmt.Errorf("verify OIDC ID token: %w", err)
	}
	var tokenClaims claims
	if err := idToken.Claims(&tokenClaims); err != nil {
		return Principal{}, fmt.Errorf("read OIDC ID token claims: %w", err)
	}
	if tokenClaims.Nonce == "" || subtle.ConstantTimeCompare([]byte(tokenClaims.Nonce), []byte(nonce)) != 1 {
		return Principal{}, errors.New("OIDC ID token nonce does not match")
	}
	principal, err := principalFromClaims(provider.issuer, tokenClaims, "")
	if err != nil {
		return Principal{}, err
	}
	if userInfo, userInfoErr := provider.oidcProvider.UserInfo(ctx, oauth2.StaticTokenSource(token)); userInfoErr == nil {
		var extra claims
		if userInfo.Claims(&extra) == nil {
			if avatarURL := firstHTTPSURL(extra.Picture, extra.AvatarURL); avatarURL != "" {
				principal.AvatarURL = avatarURL
			}
		}
	}
	return principal, nil
}
