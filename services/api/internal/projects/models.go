// Package projects implements repository project boards backed by PostgreSQL.
package projects

import (
	"context"
	"errors"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

var (
	ErrInvalidInput   = errors.New("invalid project input")
	ErrColumnNotEmpty = errors.New("project column contains items")
)

type RepositoryRef struct {
	ID             string
	OrganizationID string
}

type ProjectSummary struct {
	ID          string    `json:"id"`
	Number      int64     `json:"number"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	State       string    `json:"state"`
	CreatedBy   string    `json:"createdBy"`
	ColumnCount int64     `json:"columnCount"`
	ItemCount   int64     `json:"itemCount"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type Project struct {
	ProjectSummary
	Columns        []Column `json:"columns"`
	ViewerCanWrite bool     `json:"viewerCanWrite"`
}

type Column struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Position  int64     `json:"position"`
	Items     []Item    `json:"items"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Item struct {
	ID        string    `json:"id"`
	ColumnID  string    `json:"columnId"`
	Kind      string    `json:"kind"`
	Number    *int64    `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	Author    string    `json:"author"`
	Position  int64     `json:"position"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ProjectInput struct {
	Title       string
	Description string
	State       string
}

type ProjectUpdate struct {
	Title       *string
	Description *string
	State       *string
}

type ColumnInput struct {
	Name string
}

type ItemInput struct {
	ColumnID           string
	Kind               string
	IssueNumber        *int64
	MergeRequestNumber *int64
	Title              string
	Body               string
}

type ItemUpdate struct {
	ColumnID *string
	Title    *string
	Body     *string
}

type Store interface {
	List(context.Context, string) ([]ProjectSummary, error)
	Get(context.Context, string, int64) (Project, error)
	Create(context.Context, platform.User, RepositoryRef, ProjectInput) (Project, error)
	Update(context.Context, platform.User, RepositoryRef, int64, ProjectUpdate) (Project, error)
	Delete(context.Context, platform.User, RepositoryRef, int64) error
	CreateColumn(context.Context, platform.User, RepositoryRef, int64, ColumnInput) (Project, error)
	UpdateColumn(context.Context, platform.User, RepositoryRef, int64, string, ColumnInput) (Project, error)
	DeleteColumn(context.Context, platform.User, RepositoryRef, int64, string) error
	CreateItem(context.Context, platform.User, RepositoryRef, int64, ItemInput) (Project, error)
	UpdateItem(context.Context, platform.User, RepositoryRef, int64, string, ItemUpdate) (Project, error)
	DeleteItem(context.Context, platform.User, RepositoryRef, int64, string) error
}
