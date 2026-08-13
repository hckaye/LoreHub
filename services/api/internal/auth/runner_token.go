package auth

import (
	"errors"
	"strings"
)

const (
	runnerRegistrationTokenPrefix = "lhrr_"
	runnerCredentialPrefix        = "lhr_"
)

var ErrInvalidRunnerToken = errors.New("runner token is invalid")

func NewRunnerRegistrationToken(secrets *SecretCodec) (string, []byte, error) {
	return newRunnerToken(secrets, runnerRegistrationTokenPrefix)
}

func NewRunnerCredential(secrets *SecretCodec) (string, []byte, error) {
	return newRunnerToken(secrets, runnerCredentialPrefix)
}

func ValidRunnerRegistrationToken(raw string) bool {
	return validRunnerToken(raw, runnerRegistrationTokenPrefix)
}

func ValidRunnerCredential(raw string) bool {
	return validRunnerToken(raw, runnerCredentialPrefix)
}

func newRunnerToken(secrets *SecretCodec, prefix string) (string, []byte, error) {
	if secrets == nil {
		return "", nil, errors.New("runner token secret codec is required")
	}
	random, err := randomToken()
	if err != nil {
		return "", nil, err
	}
	raw := prefix + random
	return raw, secrets.Digest(raw), nil
}

func validRunnerToken(raw string, prefix string) bool {
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
