package reviewthreads

import (
	"context"
	"errors"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

var ErrInvalidInput = errors.New("invalid review thread input")

type RepositoryRef struct {
	ID             string
	OrganizationID string
}

type Side string

const (
	SideLeft  Side = "left"
	SideRight Side = "right"
)

type Comment struct {
	ID              string     `json:"id"`
	Author          string     `json:"author"`
	Body            string     `json:"body"`
	Deleted         bool       `json:"deleted"`
	Version         int        `json:"version"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	EditedAt        *time.Time `json:"editedAt,omitempty"`
	ViewerCanUpdate bool       `json:"viewerCanUpdate"`
	authorID        string
}

type Thread struct {
	ID               string     `json:"id"`
	Path             string     `json:"path"`
	Side             Side       `json:"side"`
	LineNumber       int        `json:"lineNumber"`
	LineContent      string     `json:"lineContent"`
	BaseRevision     string     `json:"baseRevision"`
	HeadRevision     string     `json:"headRevision"`
	Outdated         bool       `json:"outdated"`
	Resolved         bool       `json:"resolved"`
	Version          int        `json:"version"`
	CreatedBy        string     `json:"createdBy"`
	ResolvedBy       *string    `json:"resolvedBy,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
	ResolvedAt       *time.Time `json:"resolvedAt,omitempty"`
	ViewerCanResolve bool       `json:"viewerCanResolve"`
	Comments         []Comment  `json:"comments"`
	createdByID      string
	mergeAuthorID    string
}

type CreateInput struct {
	Path                 string
	Side                 Side
	LineNumber           int
	Body                 string
	ExpectedBaseRevision string
	ExpectedHeadRevision string
	LineContent          string
}

type Store interface {
	List(context.Context, string, int64) ([]Thread, error)
	Create(context.Context, platform.User, RepositoryRef, int64, CreateInput) (Thread, error)
	Reply(context.Context, platform.User, RepositoryRef, int64, string, string) (Comment, error)
	UpdateComment(
		context.Context, platform.User, RepositoryRef, int64, string, string, string, int,
	) (Comment, error)
	DeleteComment(context.Context, platform.User, RepositoryRef, int64, string, string, int) error
	SetResolved(context.Context, platform.User, RepositoryRef, int64, string, bool, int) (Thread, error)
}

type ThreadList struct {
	Threads []Thread `json:"threads"`
}
