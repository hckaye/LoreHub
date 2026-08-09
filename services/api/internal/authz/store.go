package authz

import (
	"context"
	"errors"
	"time"
)

var (
	ErrSessionNotFound    = errors.New("Lore auth session not found")
	ErrSessionState       = errors.New("Lore auth session state does not match")
	ErrSessionRateLimited = errors.New("Lore auth session polling too frequently")
	ErrSessionConsumed    = errors.New("Lore auth session was already consumed")
)

type ResourcePermissions struct {
	ResourceID  string
	Permissions []string
}

type UserInfo struct {
	ID              string
	Username        string
	DisplayName     string
	ProviderSubject string
}

type PolicyCheck struct {
	UserID                 string
	ResourceID             string
	Operation              string
	BranchID               string
	BranchName             string
	CurrentRevision        string
	ProposedRevision       string
	OperationAuthorization string
}

type PolicyDecision struct {
	Allowed bool
}

type AuthSessionPoll struct {
	UserID     string
	Ready      bool
	Consumed   bool
	RetryAfter time.Duration
}

type SessionStore interface {
	CreateLoreAuthSession(
		ctx context.Context,
		id string,
		codeDigest []byte,
		clientStateDigest []byte,
		expiresAt time.Time,
	) error
	ConfirmLoreAuthSession(ctx context.Context, codeDigest []byte, userID string) error
	PollLoreAuthSession(
		ctx context.Context,
		codeDigest []byte,
		clientStateDigest []byte,
	) (AuthSessionPoll, error)
}

type Store interface {
	EffectivePermissions(ctx context.Context, userID string, resourceID string) (
		ResourcePermissions,
		error,
	)
	ListResourcePermissions(
		ctx context.Context,
		userID string,
		resourceFilter string,
		pageSize int,
		pageToken string,
	) ([]ResourcePermissions, string, error)
	UserInfo(ctx context.Context, userID string) (UserInfo, error)
	UserInfoForResource(
		ctx context.Context,
		resourceID string,
		userIDs []string,
	) ([]UserInfo, error)
	UserInfoByDisplayName(
		ctx context.Context,
		resourceID string,
		displayName string,
	) (UserInfo, error)
	ProviderSubject(ctx context.Context, userID string) (string, error)
	CheckPolicy(ctx context.Context, check PolicyCheck) (PolicyDecision, error)
}
