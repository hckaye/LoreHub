package auth

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestOIDCProviderExchangesAndVerifiesIDToken(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	validIDToken := ""
	wrongIDAudienceToken := ""
	validAccessToken := ""
	wrongAccessAudienceToken := ""
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/.well-known/openid-configuration":
			writeJSONForTest(writer, map[string]string{
				"issuer":                 server.URL,
				"authorization_endpoint": server.URL + "/authorize",
				"token_endpoint":         server.URL + "/token",
				"jwks_uri":               server.URL + "/jwks",
			})
		case "/jwks":
			writeJSONForTest(writer, map[string]any{"keys": []map[string]string{{
				"kty": "RSA",
				"use": "sig",
				"alg": "RS256",
				"kid": "test-key",
				"n":   base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privateKey.PublicKey.E)).Bytes()),
			}}})
		case "/token":
			if err := request.ParseForm(); err != nil {
				http.Error(writer, "bad form", http.StatusBadRequest)
				return
			}
			if request.Form.Get("code_verifier") != "code-verifier" {
				http.Error(writer, "missing PKCE verifier", http.StatusBadRequest)
				return
			}
			idToken := validIDToken
			switch request.Form.Get("code") {
			case "wrong-id-audience":
				idToken = wrongIDAudienceToken
			case "access-token-as-id":
				idToken = validAccessToken
			}
			writeJSONForTest(writer, map[string]string{
				"access_token": validAccessToken,
				"token_type":   "Bearer",
				"id_token":     idToken,
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	validIDToken = signedToken(t, privateKey, map[string]any{
		"iss":                server.URL,
		"sub":                "subject-1",
		"aud":                "lorehub-web",
		"nonce":              "nonce-1",
		"preferred_username": "alice",
		"name":               "Alice",
		"email":              "alice@example.com",
		"picture":            "https://avatars.example/alice.png",
		"exp":                time.Now().Add(5 * time.Minute).Unix(),
		"iat":                time.Now().Add(-time.Minute).Unix(),
	})
	wrongIDAudienceToken = signedToken(t, privateKey, map[string]any{
		"iss":   server.URL,
		"sub":   "subject-1",
		"aud":   "lorehub-api",
		"nonce": "nonce-1",
		"exp":   time.Now().Add(5 * time.Minute).Unix(),
		"iat":   time.Now().Add(-time.Minute).Unix(),
	})
	validAccessToken = signedToken(t, privateKey, map[string]any{
		"iss":                server.URL,
		"sub":                "subject-1",
		"aud":                "lorehub-api",
		"preferred_username": "alice",
		"name":               "Alice",
		"email":              "alice@example.com",
		"exp":                time.Now().Add(5 * time.Minute).Unix(),
		"iat":                time.Now().Add(-time.Minute).Unix(),
	})
	wrongAccessAudienceToken = signedToken(t, privateKey, map[string]any{
		"iss": server.URL,
		"sub": "subject-1",
		"aud": "lorehub-web",
		"exp": time.Now().Add(5 * time.Minute).Unix(),
		"iat": time.Now().Add(-time.Minute).Unix(),
	})

	provider, err := NewOIDCProvider(t.Context(), OIDCConfig{
		Issuer:       server.URL,
		ClientID:     "lorehub-web",
		Audience:     "lorehub-api",
		ClientSecret: "client-secret",
		RedirectURL:  server.URL + "/auth/callback",
	})
	if err != nil {
		t.Fatal(err)
	}
	authorizationURL, err := url.Parse(provider.AuthorizationURL("state-1", "challenge-1", "nonce-1", ""))
	if err != nil {
		t.Fatal(err)
	}
	if authorizationURL.Query().Get("code_challenge_method") != "S256" ||
		authorizationURL.Query().Get("code_challenge") != "challenge-1" ||
		authorizationURL.Query().Get("nonce") != "nonce-1" {
		t.Fatalf("authorization request omitted PKCE or nonce: %s", authorizationURL)
	}
	registrationURL, err := url.Parse(provider.AuthorizationURL("state-1", "challenge-1", "nonce-1", RegistrationPrompt))
	if err != nil {
		t.Fatal(err)
	}
	if registrationURL.Query().Get("prompt") != RegistrationPrompt {
		t.Fatalf("registration request omitted prompt=create: %s", registrationURL)
	}
	unsupportedURL, err := url.Parse(provider.AuthorizationURL("state-1", "challenge-1", "nonce-1", "login"))
	if err != nil {
		t.Fatal(err)
	}
	if unsupportedURL.Query().Get("prompt") != "" {
		t.Fatal("provider forwarded an unsupported prompt")
	}
	hintedURL, err := url.Parse(provider.AuthorizationURLForProvider(
		"state-1", "challenge-1", "nonce-1", "", "github",
	))
	if err != nil {
		t.Fatal(err)
	}
	if hintedURL.Query().Get("kc_idp_hint") != "github" {
		t.Fatalf("provider hint was not forwarded: %s", hintedURL)
	}

	principal, err := provider.Exchange(t.Context(), "authorization-code", "code-verifier", "nonce-1")
	if err != nil {
		t.Fatal(err)
	}
	if principal.Issuer != server.URL || principal.Subject != "subject-1" || principal.Username != "alice" ||
		principal.AvatarURL != "https://avatars.example/alice.png" || principal.LoreAccessToken != "" {
		t.Fatalf("unexpected principal: %#v", principal)
	}
	if _, err := provider.Exchange(t.Context(), "authorization-code", "code-verifier", "wrong-nonce"); err == nil {
		t.Fatal("expected a mismatched nonce to be rejected")
	}
	accessPrincipal, err := provider.Authenticate(t.Context(), "Bearer "+validAccessToken)
	if err != nil {
		t.Fatalf("expected access token audience to pass: %v", err)
	}
	if accessPrincipal.Subject != "subject-1" || accessPrincipal.LoreAccessToken != validAccessToken {
		t.Fatalf("unexpected access-token principal: %#v", accessPrincipal)
	}
	if _, err := provider.Authenticate(t.Context(), "Bearer "+validIDToken); err == nil {
		t.Fatal("expected an ID token with the web audience to fail access-token verification")
	}
	if _, err := provider.Authenticate(t.Context(), "Bearer "+wrongAccessAudienceToken); err == nil {
		t.Fatal("expected an access token with the web audience to be rejected")
	}
	if _, err := provider.Exchange(t.Context(), "wrong-id-audience", "code-verifier", "nonce-1"); err == nil {
		t.Fatal("expected an ID token with the API audience to be rejected")
	}
	if _, err := provider.Exchange(t.Context(), "access-token-as-id", "code-verifier", "nonce-1"); err == nil {
		t.Fatal("expected an access token to be rejected as an ID token")
	}

	bearerAuthenticator, err := NewOIDC(t.Context(), server.URL, "lorehub-api")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bearerAuthenticator.Authenticate(t.Context(), "Bearer "+validAccessToken); err != nil {
		t.Fatalf("bearer-only authenticator rejected the API audience: %v", err)
	}
	if _, err := bearerAuthenticator.Authenticate(t.Context(), "Bearer "+validIDToken); err == nil {
		t.Fatal("bearer-only authenticator accepted the web audience")
	}
}

func TestNewOIDCProviderRequiresAudience(t *testing.T) {
	_, err := NewOIDCProvider(t.Context(), OIDCConfig{
		Issuer:   "https://identity.example/realms/lorehub",
		ClientID: "lorehub-web",
	})
	if err == nil {
		t.Fatal("expected an OIDC audience requirement")
	}
}

func signedToken(t *testing.T, privateKey *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	encode := func(value any) string {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(encoded)
	}
	header := encode(map[string]string{"alg": "RS256", "kid": "test-key", "typ": "JWT"})
	payload := encode(claims)
	input := header + "." + payload
	digest := sha256.Sum256([]byte(input))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func writeJSONForTest(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(value)
}
