package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"golang.org/x/crypto/argon2"
)

// PasswordIssuer identifies built-in password accounts in user_identities.
// It is not a URL, so it cannot collide with an external OIDC issuer.
const PasswordIssuer = "lorehub"

var (
	ErrInvalidCredentials = errors.New("the credentials are invalid")
	ErrPasswordPolicy     = errors.New("the password does not meet the policy")
)

const (
	argonTimeCost   = 2
	argonMemoryKiB  = 19 * 1024
	argonThreads    = 1
	argonSaltLength = 16
	argonKeyLength  = 32

	MinPasswordLength = 12
	MaxPasswordLength = 512
)

const (
	lockoutThreshold = 5
	lockoutBaseDelay = 30 * time.Second
	LockoutMaxDelay  = 15 * time.Minute
)

func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", errors.New("generate password salt")
	}
	key := argon2.IDKey([]byte(password), salt, argonTimeCost, argonMemoryKiB, argonThreads, argonKeyLength)
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemoryKiB,
		argonTimeCost,
		argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

func VerifyPassword(encoded string, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	var memory uint32
	var timeCost uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &timeCost, &threads); err != nil {
		return false
	}
	if memory == 0 || memory > 1<<21 || timeCost == 0 || timeCost > 16 || threads == 0 {
		return false
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return false
	}
	expected, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil || len(expected) == 0 {
		return false
	}
	key := argon2.IDKey([]byte(password), salt, timeCost, memory, threads, uint32(len(expected)))
	return subtle.ConstantTimeCompare(key, expected) == 1
}

var fakeVerificationHash = mustHashPassword("lorehub-timing-equalizer")

func mustHashPassword(password string) string {
	hash, err := HashPassword(password)
	if err != nil {
		panic(err)
	}
	return hash
}

// EqualizeVerificationTiming performs a discarded hash verification so a
// missing account takes as long as a wrong password.
func EqualizeVerificationTiming(password string) {
	VerifyPassword(fakeVerificationHash, password)
}

// ValidatePassword enforces the password policy: at least 12 characters with
// uppercase and lowercase letters, a number, and a symbol, and the password
// cannot contain the username or the email address.
func ValidatePassword(password string, username string, email string) error {
	if len(password) < MinPasswordLength || len(password) > MaxPasswordLength {
		return ErrPasswordPolicy
	}
	var hasUpper, hasLower, hasDigit, hasSymbol bool
	for _, character := range password {
		switch {
		case unicode.IsUpper(character):
			hasUpper = true
		case unicode.IsLower(character):
			hasLower = true
		case unicode.IsDigit(character):
			hasDigit = true
		default:
			hasSymbol = true
		}
	}
	if !hasUpper || !hasLower || !hasDigit || !hasSymbol {
		return ErrPasswordPolicy
	}
	lowered := strings.ToLower(password)
	if username = strings.ToLower(strings.TrimSpace(username)); username != "" &&
		strings.Contains(lowered, username) {
		return ErrPasswordPolicy
	}
	if email = strings.ToLower(strings.TrimSpace(email)); email != "" && strings.Contains(lowered, email) {
		return ErrPasswordPolicy
	}
	return nil
}

// LockoutDelay returns how long an account stays locked after the given
// number of consecutive failures: nothing before five failures, then an
// exponentially growing delay capped at LockoutMaxDelay.
func LockoutDelay(failures int) time.Duration {
	if failures < lockoutThreshold {
		return 0
	}
	exponent := failures - lockoutThreshold
	if exponent > 10 {
		return LockoutMaxDelay
	}
	delay := lockoutBaseDelay << uint(exponent)
	if delay > LockoutMaxDelay {
		return LockoutMaxDelay
	}
	return delay
}
