package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/auth"
)

func TestIdentityQueryHelpers(t *testing.T) {
	t.Parallel()
	if limit, err := queryLimit("", 20, 50); err != nil || limit != 20 {
		t.Fatalf("default limit = %d, %v", limit, err)
	}
	if _, err := queryLimit("0", 20, 50); err == nil {
		t.Fatal("zero limit should be rejected")
	}
	if value, err := optionalBool("true"); err != nil || !value {
		t.Fatalf("optional bool = %t, %v", value, err)
	}
	if validOptionalText(pointerToString("hello"), 4) {
		t.Fatal("overlong optional text should be rejected")
	}
	if !validVisibilityPointer(pointerToString("private")) || validVisibilityPointer(pointerToString("unknown")) {
		t.Fatal("visibility helper accepted an invalid value")
	}
	if !validOptionalURL(pointerToString("https://lore.example/profile")) ||
		validOptionalURL(pointerToString("javascript:alert(1)")) {
		t.Fatal("URL helper accepted an unsafe or rejected a safe URL")
	}
}

func TestConfiguredLoginProvidersAreReturnedWithoutSecrets(t *testing.T) {
	t.Parallel()
	codec, err := auth.NewSecretCodec("test authentication secret")
	if err != nil {
		t.Fatal(err)
	}
	provider := &fakeLoginProvider{}
	authenticationStore := &fakeAuthenticationStore{}
	handler := New(
		fakeStore{},
		fakeLore{},
		auth.DisabledAuthenticator{},
		healthy{},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithAuthentication(AuthOptions{
			LoginProvider: provider,
			LoginStore:    authenticationStore,
			SessionStore:  authenticationStore,
			Secrets:       codec,
			PublicOrigin:  "https://app.example",
			SessionCookie: SessionCookieOptions{Name: "session", Path: "/", Secure: true},
		}),
		WithConfiguredLoginProviders([]string{"github"}),
	)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/providers", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("providers status = %d, body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Providers []struct {
			ID string `json:"id"`
		} `json:"providers"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Providers) != 2 || payload.Providers[0].ID != "password" || payload.Providers[1].ID != "github" {
		t.Fatalf("unexpected providers: %+v", payload.Providers)
	}
	request = httptest.NewRequest(http.MethodGet, "/auth/login?provider=google", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unconfigured provider status = %d, body=%s", response.Code, response.Body.String())
	}
}

func pointerToString(value string) *string {
	return &value
}
