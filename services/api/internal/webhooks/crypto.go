package webhooks

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var keyIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type SecretBox struct {
	aead  cipher.AEAD
	keyID string
}

func NewSecretBox(keyID string, encodedKey string) (*SecretBox, error) {
	if !keyIDPattern.MatchString(keyID) {
		return nil, errors.New("webhook encryption key ID is invalid")
	}
	if encodedKey == "" || strings.TrimSpace(encodedKey) != encodedKey ||
		strings.ContainsAny(encodedKey, "\r\n") {
		return nil, errors.New("webhook encryption key must be unbroken base64")
	}
	key, err := base64.StdEncoding.Strict().DecodeString(encodedKey)
	if err != nil || len(key) != 32 {
		clear(key)
		return nil, errors.New("webhook encryption key must decode to exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	clear(key)
	if err != nil {
		return nil, fmt.Errorf("create webhook cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create webhook GCM: %w", err)
	}
	return &SecretBox{aead: aead, keyID: keyID}, nil
}

func (box *SecretBox) Seal(webhookID string, repositoryID string, secret string) ([]byte, []byte, string, error) {
	if box == nil || box.aead == nil {
		return nil, nil, "", errors.New("webhook encryption is not configured")
	}
	nonce := make([]byte, box.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, "", fmt.Errorf("generate webhook secret nonce: %w", err)
	}
	plaintext := []byte(secret)
	ciphertext := box.aead.Seal(nil, nonce, plaintext, webhookAAD(webhookID, repositoryID))
	clear(plaintext)
	return ciphertext, nonce, box.keyID, nil
}

func (box *SecretBox) Open(
	webhookID string,
	repositoryID string,
	ciphertext []byte,
	nonce []byte,
	keyID string,
) ([]byte, error) {
	if box == nil || box.aead == nil || keyID != box.keyID {
		return nil, errors.New("webhook encryption key is unavailable")
	}
	plaintext, err := box.aead.Open(nil, nonce, ciphertext, webhookAAD(webhookID, repositoryID))
	if err != nil {
		return nil, errors.New("webhook secret authentication failed")
	}
	return plaintext, nil
}

func webhookAAD(webhookID string, repositoryID string) []byte {
	return []byte("lorehub-webhook-secret-v1\x00" + webhookID + "\x00" + repositoryID)
}
