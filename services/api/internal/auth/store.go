package auth

import (
	"context"
	"time"
)

type LoginTransaction struct {
	ID                 string
	StateDigest        []byte
	CodeVerifierDigest []byte
	NonceDigest        []byte
	ReturnTo           string
	CreatedAt          time.Time
	ExpiresAt          time.Time
}

type Session struct {
	ID          string
	UserID      string
	Username    string
	DisplayName string
	Email       string
	Locale      string
	CSRFDigest  []byte
	CreatedAt   time.Time
	ExpiresAt   time.Time
	LastSeenAt  time.Time
}

type LoginTransactionStore interface {
	CreateLoginTransaction(ctx context.Context, transaction LoginTransaction) error
	ConsumeLoginTransaction(ctx context.Context, stateDigest []byte, now time.Time) (LoginTransaction, error)
}

type SessionStore interface {
	CreateSession(
		ctx context.Context,
		userID string,
		tokenDigest []byte,
		csrfDigest []byte,
		expiresAt time.Time,
	) (Session, error)
	LookupSession(ctx context.Context, tokenDigest []byte, now time.Time) (Session, error)
	RevokeSession(ctx context.Context, tokenDigest []byte, now time.Time) error
}

type CleanupStore interface {
	CleanupExpiredAuthentication(ctx context.Context, now time.Time) error
}
