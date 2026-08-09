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
