package lore

import (
	"context"
	"time"
)

type Repository struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	URL           string    `json:"url"`
	DefaultBranch string    `json:"defaultBranch"`
	Creator       string    `json:"creator"`
	CreatedAt     time.Time `json:"createdAt"`
}

type RepositoryRef struct {
	CacheKey string
	URL      string
}

type Credential struct {
	Token    string
	AuthURL  string
	Identity string
}

type Branch struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Category       string    `json:"category"`
	LatestRevision string    `json:"latestRevision"`
	Creator        string    `json:"creator"`
	CreatedAt      time.Time `json:"createdAt"`
	Current        bool      `json:"current"`
	Archived       bool      `json:"archived"`
}

type Client interface {
	RepositoryInfo(ctx context.Context, repositoryURL string, identity string) (Repository, error)
	Branches(ctx context.Context, repository RepositoryRef, identity string) ([]Branch, error)
}

type CredentialBranchClient interface {
	BranchesWithCredential(ctx context.Context, repository RepositoryRef, credential Credential) ([]Branch, error)
}

type RevisionClient interface {
	CloneRevision(
		ctx context.Context,
		repository RepositoryRef,
		identity string,
		revision string,
		destination string,
	) error
}

type CredentialRevisionClient interface {
	CloneRevisionWithCredential(
		ctx context.Context,
		repository RepositoryRef,
		credential Credential,
		revision string,
		destination string,
	) error
}
