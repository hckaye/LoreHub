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
	validToken := ""
	wrongAudienceToken := ""
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
			token := validToken
			if request.Form.Get("code") == "wrong-audience" {
				token = wrongAudienceToken
			}
			writeJSONForTest(writer, map[string]string{
				"access_token": "access-token",
				"token_type":   "Bearer",
				"id_token":     token,
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	validToken = signedIDToken(t, privateKey, map[string]any{
		"iss":                server.URL,
		"sub":                "subject-1",
		"aud":                "client-1",
		"nonce":              "nonce-1",
		"preferred_username": "alice",
		"name":               "Alice",
		"email":              "alice@example.com",
		"exp":                time.Now().Add(5 * time.Minute).Unix(),
		"iat":                time.Now().Add(-time.Minute).Unix(),
	})
	wrongAudienceToken = signedIDToken(t, privateKey, map[string]any{
		"iss":   server.URL,
		"sub":   "subject-1",
		"aud":   "different-client",
		"nonce": "nonce-1",
		"exp":   time.Now().Add(5 * time.Minute).Unix(),
		"iat":   time.Now().Add(-time.Minute).Unix(),
	})

	provider, err := NewOIDCProvider(t.Context(), OIDCConfig{
		Issuer:       server.URL,
		ClientID:     "client-1",
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

	principal, err := provider.Exchange(t.Context(), "authorization-code", "code-verifier", "nonce-1")
	if err != nil {
		t.Fatal(err)
	}
	if principal.Issuer != server.URL || principal.Subject != "subject-1" || principal.Username != "alice" ||
		principal.LoreAccessToken != "" {
		t.Fatalf("unexpected principal: %#v", principal)
	}
	if _, err := provider.Exchange(t.Context(), "authorization-code", "code-verifier", "wrong-nonce"); err == nil {
		t.Fatal("expected a mismatched nonce to be rejected")
	}
	if _, err := provider.Exchange(t.Context(), "wrong-audience", "code-verifier", "nonce-1"); err == nil {
		t.Fatal("expected a mismatched audience to be rejected")
	}
}

func signedIDToken(t *testing.T, privateKey *rsa.PrivateKey, claims map[string]any) string {
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
