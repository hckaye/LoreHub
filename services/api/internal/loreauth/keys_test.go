package loreauth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/lorehub/lorehub/services/api/internal/authz"
)

func newTestTokenService(t *testing.T) (*TokenService, *RSAKeyProvider) {
	t.Helper()
	provider, err := NewRSAKeyProvider(filepath.Join(t.TempDir(), "current.pem"), "", "current", "", true)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewTokenService(provider, "auth.lorehub.example", "lorehub.example",
		"production", "keycloak", 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return service, provider
}

func TestMintResourceTokenUsesLoreClaimsAndExactScope(t *testing.T) {
	service, _ := newTestTokenService(t)
	raw, expiresAt, err := service.MintResourceToken(
		authz.UserInfo{ID: "user-1", Username: "alice", DisplayName: "Alice"},
		[]LoreResourcePermission{{
			ResourceID: "urc-0123456789abcdef0123456789abcdef",
			Permission: []string{authz.PermissionRead, authz.PermissionWrite},
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if expiresAt.Before(time.Now().UTC().Add(4 * time.Minute)) {
		t.Fatalf("token expires too soon: %s", expiresAt)
	}
	verified, err := service.Verify(raw)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Claims.Subject != "user-1" || verified.Claims.Issuer != "auth.lorehub.example" ||
		verified.Claims.Environment != "production" || verified.Claims.IDP != "keycloak" ||
		verified.Claims.IsServiceAccount {
		t.Fatalf("unexpected claims: %#v", verified.Claims)
	}
	if len(verified.Claims.Audience) != 1 || verified.Claims.Audience[0] != "lorehub.example" {
		t.Fatalf("unexpected audience: %v", verified.Claims.Audience)
	}
	if len(verified.Claims.Resources) != 1 || verified.Claims.Resources[0].ResourceID == "urc-*" {
		t.Fatalf("unexpected resources: %#v", verified.Claims.Resources)
	}
	var payload map[string]any
	parts := splitJWT(t, raw)
	if err := json.Unmarshal(parts[1], &payload); err != nil {
		t.Fatal(err)
	}
	if value, ok := payload["is_service_account"].(bool); !ok || value {
		t.Fatalf("is_service_account must be explicit false: %#v", payload["is_service_account"])
	}
	if _, _, err := service.MintResourceToken(authz.UserInfo{ID: "u"}, []LoreResourcePermission{{
		ResourceID: "urc-*", Permission: []string{authz.PermissionRead},
	}}); err == nil {
		t.Fatal("wildcard resource was accepted")
	}
}

func TestVerifyRejectsIssuerAudienceKidAndExpiry(t *testing.T) {
	service, provider := newTestTokenService(t)
	base := jwt.Claims{
		Subject: "user-1", Issuer: "wrong", IssuedAt: jwt.NewNumericDate(time.Now().UTC()),
		Expiry: jwt.NewNumericDate(time.Now().UTC().Add(time.Minute)), Audience: jwt.Audience{"wrong"},
	}
	for name, claims := range map[string]jwt.Claims{
		"wrong issuer":   base,
		"wrong audience": func() jwt.Claims { value := base; value.Issuer = "auth.lorehub.example"; return value }(),
		"expired": func() jwt.Claims {
			value := base
			value.Issuer = "auth.lorehub.example"
			value.Audience = jwt.Audience{"lorehub.example"}
			value.Expiry = jwt.NewNumericDate(time.Now().UTC().Add(-time.Minute))
			return value
		}(),
	} {
		raw := signClaims(t, provider.Current().Key, provider.Current().KeyID, claims)
		if _, err := service.Verify(raw); err == nil {
			t.Fatalf("%s token was accepted", name)
		}
	}
	valid := base
	valid.Issuer = "auth.lorehub.example"
	valid.Audience = jwt.Audience{"lorehub.example"}
	valid.Expiry = jwt.NewNumericDate(time.Now().UTC().Add(time.Minute))
	raw := signClaims(t, provider.Current().Key, "unknown", valid)
	if _, err := service.Verify(raw); err == nil {
		t.Fatal("unknown kid was accepted")
	}
}

func TestJWKSRetainsPreviousPublicKeyDuringRotation(t *testing.T) {
	directory := t.TempDir()
	currentPath := filepath.Join(directory, "current.pem")
	previousPath := filepath.Join(directory, "previous.pem")
	current, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	previous, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	writePEM(t, currentPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(current), 0o600)
	previousPublic, err := x509.MarshalPKIXPublicKey(previous.Public())
	if err != nil {
		t.Fatal(err)
	}
	writePEM(t, previousPath, "PUBLIC KEY", previousPublic, 0o600)
	provider, err := NewRSAKeyProvider(currentPath, "", "new", "old="+previousPath, false)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewTokenService(provider, "auth.lorehub.example", "lorehub.example",
		"production", "keycloak", 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	claims := jwt.Claims{
		Subject: "user-1", Issuer: "auth.lorehub.example",
		IssuedAt: jwt.NewNumericDate(time.Now().UTC()),
		Expiry:   jwt.NewNumericDate(time.Now().UTC().Add(time.Minute)), Audience: jwt.Audience{"lorehub.example"},
	}
	raw := signClaims(t, previous, "old", LoreClaims{
		Claims: claims,
		IDP:    "keycloak",
		Resources: []LoreResourcePermission{{
			ResourceID: "urc-0123456789abcdef0123456789abcdef",
			Permission: []string{authz.PermissionRead},
		}},
	})
	if _, err := service.Verify(raw); err != nil {
		t.Fatalf("previous key token was not accepted during overlap: %v", err)
	}
	keys, ok := service.JWKS()["keys"].([]jose.JSONWebKey)
	if !ok || len(keys) != 2 || keys[0].KeyID != "new" || keys[1].KeyID != "old" {
		t.Fatalf("unexpected JWKS overlap: %#v", service.JWKS())
	}
}

func signClaims(t *testing.T, key any, kid string, claims any) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", kid))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func splitJWT(t *testing.T, raw string) [][]byte {
	t.Helper()
	parts := make([][]byte, 0, 3)
	for _, part := range strings.Split(raw, ".") {
		decoded, err := base64.RawURLEncoding.DecodeString(part)
		if err != nil {
			t.Fatal(err)
		}
		parts = append(parts, decoded)
	}
	return parts
}

func writePEM(t *testing.T, path string, kind string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: data}), mode); err != nil {
		t.Fatal(err)
	}
}
