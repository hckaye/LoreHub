package auth

import (
	"errors"
	"testing"
)

func TestBearerToken(t *testing.T) {
	t.Parallel()
	token, err := bearerToken("Bearer example-token")
	if err != nil {
		t.Fatalf("bearerToken returned an error: %v", err)
	}
	if token != "example-token" {
		t.Fatalf("expected token to be returned, got %q", token)
	}
}

func TestBearerTokenRejectsMissingValue(t *testing.T) {
	t.Parallel()
	_, err := bearerToken("Basic example-token")
	if !errors.Is(err, ErrMissingToken) {
		t.Fatalf("expected ErrMissingToken, got %v", err)
	}
}

func TestFirstHTTPSURLAcceptsProviderAvatars(t *testing.T) {
	t.Parallel()
	got := firstHTTPSURL(
		"javascript:alert(1)",
		"http://avatars.example/alice.png",
		"https://avatars.githubusercontent.com/u/1?v=4",
	)
	if got != "https://avatars.githubusercontent.com/u/1?v=4" {
		t.Fatalf("unexpected avatar URL %q", got)
	}
	if firstHTTPSURL("", "data:image/png;base64,abc") != "" {
		t.Fatal("expected non-https avatar URLs to be rejected")
	}
}
