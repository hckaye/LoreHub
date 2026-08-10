package main

import (
	"crypto/rand"
	"crypto/rsa"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-jose/go-jose/v4"
)

func TestHasPublicSigningKey(t *testing.T) {
	if hasPublicSigningKey(nil) {
		t.Fatal("nil JWKS must not be ready")
	}
	if hasPublicSigningKey(map[string]any{
		"keys": []jose.JSONWebKey{{KeyID: "current"}},
	}) {
		t.Fatal("an invalid JWKS key must not be ready")
	}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	if !hasPublicSigningKey(map[string]any{
		"keys": []jose.JSONWebKey{{
			Key: &privateKey.PublicKey, KeyID: "current", Use: "sig", Algorithm: string(jose.RS256),
		}},
	}) {
		t.Fatal("a public JWKS key should be ready")
	}
}

func TestRequiredFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "required")
	if err := requiredFile(file); err == nil {
		t.Fatal("missing file should not be ready")
	}
	if err := os.WriteFile(file, []byte("material"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := requiredFile(file); err != nil {
		t.Fatalf("regular non-empty file was rejected: %v", err)
	}
	if err := requiredFile(filepath.Dir(file)); err == nil {
		t.Fatal("directory should not be accepted as a prerequisite")
	}
}
