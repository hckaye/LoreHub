// Package wiki implements versioned repository documentation backed by PostgreSQL.
package wiki

import (
	"context"
	"errors"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

var ErrInvalidInput = errors.New("invalid wiki input")

type RepositoryRef struct {
	ID             string
	OrganizationID string
}

type PageSummary struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Title     string    `json:"title"`
	Version   int       `json:"version"`
	CreatedBy string    `json:"createdBy"`
	UpdatedBy string    `json:"updatedBy"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Page struct {
	PageSummary
	Body           string `json:"body"`
	ViewerCanWrite bool   `json:"viewerCanWrite"`
}

type PageList struct {
	Pages          []PageSummary `json:"pages"`
	ViewerCanWrite bool          `json:"viewerCanWrite"`
}

type Revision struct {
	Version     int       `json:"version"`
	Slug        string    `json:"slug"`
	Title       string    `json:"title"`
	Body        string    `json:"body,omitempty"`
	EditSummary string    `json:"editSummary"`
	EditedBy    string    `json:"editedBy"`
	CreatedAt   time.Time `json:"createdAt"`
}

type CreateInput struct {
	Slug        string
	Title       string
	Body        string
	EditSummary string
}

type UpdateInput struct {
	Slug            *string
	Title           *string
	Body            *string
	EditSummary     string
	ExpectedVersion int
}

type Store interface {
	List(context.Context, string, string, int) ([]PageSummary, error)
	Get(context.Context, string, string) (Page, error)
	History(context.Context, string, string, int) ([]Revision, error)
	Revision(context.Context, string, string, int) (Revision, error)
	Create(context.Context, platform.User, RepositoryRef, CreateInput) (Page, error)
	Update(context.Context, platform.User, RepositoryRef, string, UpdateInput) (Page, error)
	Delete(context.Context, platform.User, RepositoryRef, string, int) error
}
