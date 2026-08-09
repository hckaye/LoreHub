package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
)

type SecretCodec struct {
	key []byte
}

func NewSecretCodec(secret string) (*SecretCodec, error) {
	if secret == "" {
		return nil, errors.New("authentication secret is required")
	}
	key := sha256.Sum256([]byte(secret))
	return &SecretCodec{key: key[:]}, nil
}

func (codec *SecretCodec) Digest(value string) []byte {
	mac := hmac.New(sha256.New, codec.key)
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
}

func (codec *SecretCodec) Matches(value string, expected []byte) bool {
	return hmac.Equal(codec.Digest(value), expected)
}

func (codec *SecretCodec) NewState() (string, error) {
	return randomToken()
}

func (codec *SecretCodec) CodeVerifier(state string) string {
	return codec.derivedToken("pkce", state)
}

func (codec *SecretCodec) Nonce(state string) string {
	return codec.derivedToken("nonce", state)
}

func (codec *SecretCodec) CodeChallenge(codeVerifier string) string {
	digest := sha256.Sum256([]byte(codeVerifier))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func (codec *SecretCodec) NewSessionToken() (string, error) {
	return randomToken()
}

func (codec *SecretCodec) CSRFToken(sessionToken string) string {
	return codec.derivedToken("csrf", sessionToken)
}

func (codec *SecretCodec) derivedToken(label string, value string) string {
	mac := hmac.New(sha256.New, codec.key)
	_, _ = mac.Write([]byte(label))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func randomToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", errors.New("generate authentication secret")
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
