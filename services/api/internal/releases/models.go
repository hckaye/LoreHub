// Package releases implements repository releases pinned to immutable Lore revisions.
package releases

import (
	"context"
	"errors"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

var (
	ErrInvalidInput    = errors.New("invalid release input")
	ErrVersionConflict = errors.New("release version conflict")
)

type RepositoryRef struct {
	ID             string
	OrganizationID string
}

type AssetLink struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	ExternalURL string    `json:"externalUrl"`
	CreatedBy   string    `json:"createdBy"`
	CreatedAt   time.Time `json:"createdAt"`
}

type Release struct {
	ID             string      `json:"id"`
	TagName        string      `json:"tagName"`
	Title          string      `json:"title"`
	Notes          string      `json:"notes"`
	SourceBranch   string      `json:"sourceBranch"`
	Revision       string      `json:"revision"`
	State          string      `json:"state"`
	CreatedBy      string      `json:"createdBy"`
	PublishedBy    *string     `json:"publishedBy"`
	PublishedAt    *time.Time  `json:"publishedAt"`
	Version        int64       `json:"version"`
	Assets         []AssetLink `json:"assets"`
	CreatedAt      time.Time   `json:"createdAt"`
	UpdatedAt      time.Time   `json:"updatedAt"`
	ViewerCanWrite bool        `json:"viewerCanWrite"`
}

type ReleasePage struct {
	Releases       []Release `json:"releases"`
	Page           int       `json:"page"`
	PerPage        int       `json:"perPage"`
	HasNext        bool      `json:"hasNext"`
	ViewerCanWrite bool      `json:"viewerCanWrite"`
}

type CreateInput struct {
	TagName      string
	Title        string
	Notes        string
	SourceBranch string
	Revision     string
	State        string
}

type UpdateInput struct {
	Title           *string
	Notes           *string
	ExpectedVersion int64
}

type AssetInput struct {
	Name            string
	ExternalURL     string
	ExpectedVersion int64
}

type Store interface {
	List(context.Context, string, bool, int, int) (ReleasePage, error)
	Get(context.Context, string, string, bool) (Release, error)
	Create(context.Context, platform.User, RepositoryRef, CreateInput) (Release, error)
	Update(context.Context, platform.User, RepositoryRef, string, UpdateInput) (Release, error)
	Publish(context.Context, platform.User, RepositoryRef, string, int64) (Release, error)
	Delete(context.Context, platform.User, RepositoryRef, string, int64) error
	AddAsset(context.Context, platform.User, RepositoryRef, string, AssetInput) (Release, error)
	DeleteAsset(context.Context, platform.User, RepositoryRef, string, string, int64) (Release, error)
}
