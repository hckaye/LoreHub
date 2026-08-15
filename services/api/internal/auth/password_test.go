package auth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := HashPassword("Correct-Horse-Battery-9!")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("unexpected hash format: %s", hash)
	}
	if !VerifyPassword(hash, "Correct-Horse-Battery-9!") {
		t.Fatal("correct password did not verify")
	}
	if VerifyPassword(hash, "Wrong-Horse-Battery-9!") {
		t.Fatal("wrong password verified")
	}
	second, err := HashPassword("Correct-Horse-Battery-9!")
	if err != nil {
		t.Fatal(err)
	}
	if second == hash {
		t.Fatal("hashes are not salted")
	}
}

func TestVerifyPasswordRejectsMalformedHashes(t *testing.T) {
	for _, encoded := range []string{
		"",
		"plaintext",
		"$argon2i$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=18$m=19456,t=2,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=0,t=2,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=19456,t=2,p=1$!!$aGFzaA",
		"$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$",
	} {
		if VerifyPassword(encoded, "any-password") {
			t.Fatalf("malformed hash %q verified", encoded)
		}
	}
}

func TestValidatePasswordPolicy(t *testing.T) {
	if err := ValidatePassword("Sufficient-1Password", "alice", "alice@example.com"); err != nil {
		t.Fatalf("compliant password rejected: %v", err)
	}
	cases := map[string]string{
		"too short":         "Ab1!short",
		"no uppercase":      "lowercase-only-12",
		"no lowercase":      "UPPERCASE-ONLY-12",
		"no digit":          "No-Digits-Here!!",
		"no symbol":         "NoSymbolsHere123",
		"contains username": "Has-alice-Inside1!",
		"contains email":    "Xalice@example.com1!A",
		"too long":          "A1!" + strings.Repeat("a", MaxPasswordLength),
	}
	for name, password := range cases {
		if err := ValidatePassword(password, "alice", "alice@example.com"); !errors.Is(err, ErrPasswordPolicy) {
			t.Fatalf("%s: expected policy error, got %v", name, err)
		}
	}
}

func TestLockoutDelayGrowsAndCaps(t *testing.T) {
	if LockoutDelay(0) != 0 || LockoutDelay(4) != 0 {
		t.Fatal("lockout started before the threshold")
	}
	if LockoutDelay(5) != 30*time.Second {
		t.Fatalf("unexpected first delay: %s", LockoutDelay(5))
	}
	if LockoutDelay(6) != time.Minute {
		t.Fatalf("unexpected second delay: %s", LockoutDelay(6))
	}
	if LockoutDelay(20) != LockoutMaxDelay || LockoutDelay(1000) != LockoutMaxDelay {
		t.Fatal("lockout delay is not capped")
	}
}
