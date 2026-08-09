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

type CredentialClient interface {
	RepositoryInfoWithCredential(ctx context.Context, repositoryURL string, credential Credential) (Repository, error)
	BranchesWithCredential(ctx context.Context, repository RepositoryRef, credential Credential) ([]Branch, error)
	CloneWithCredential(
		ctx context.Context,
		repositoryURL string,
		revision string,
		destination string,
		credential Credential,
	) error
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
