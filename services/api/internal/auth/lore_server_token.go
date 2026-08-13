package auth

import (
	"errors"
	"strings"
)

const (
	loreServerRegistrationTokenPrefix = "lhsr_"
	loreServerCredentialPrefix        = "lhss_"
)

var (
	ErrInvalidLoreServerRegistrationToken = errors.New("Lore server registration token is invalid")
	ErrInvalidLoreServerCredential        = errors.New("Lore server credential is invalid")
)

func NewLoreServerRegistrationToken(secrets *SecretCodec) (string, []byte, error) {
	return newLoreServerToken(secrets, loreServerRegistrationTokenPrefix)
}

func NewLoreServerCredential(secrets *SecretCodec) (string, []byte, error) {
	return newLoreServerToken(secrets, loreServerCredentialPrefix)
}

func ValidLoreServerRegistrationToken(raw string) bool {
	return validLoreServerToken(raw, loreServerRegistrationTokenPrefix)
}

func ValidLoreServerCredential(raw string) bool {
	return validLoreServerToken(raw, loreServerCredentialPrefix)
}

func newLoreServerToken(secrets *SecretCodec, prefix string) (string, []byte, error) {
	if secrets == nil {
		return "", nil, errors.New("Lore server token secret codec is required")
	}
	random, err := randomToken()
	if err != nil {
		return "", nil, err
	}
	raw := prefix + random
	return raw, secrets.Digest(raw), nil
}

func validLoreServerToken(raw string, prefix string) bool {
	if len(raw) != len(prefix)+43 || !strings.HasPrefix(raw, prefix) {
		return false
	}
	for _, character := range raw[len(prefix):] {
		if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '-' || character == '_') {
			return false
		}
	}
	return true
}
