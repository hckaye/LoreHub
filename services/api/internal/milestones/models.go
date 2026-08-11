// Package milestones implements repository milestones and Issue assignment.
package milestones

import (
	"context"
	"errors"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/collab"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

var (
	ErrInvalidInput    = errors.New("invalid milestone input")
	ErrVersionConflict = errors.New("milestone version conflict")
)

type RepositoryRef struct {
	ID             string
	OrganizationID string
}

type Milestone struct {
	ID               string     `json:"id"`
	Number           int64      `json:"number"`
	Title            string     `json:"title"`
	Description      string     `json:"description"`
	State            string     `json:"state"`
	DueOn            *string    `json:"dueOn"`
	CreatedBy        string     `json:"createdBy"`
	ClosedBy         *string    `json:"closedBy"`
	ClosedAt         *time.Time `json:"closedAt"`
	OpenIssueCount   int64      `json:"openIssueCount"`
	ClosedIssueCount int64      `json:"closedIssueCount"`
	Version          int64      `json:"version"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
	ViewerCanWrite   bool       `json:"viewerCanWrite"`
}

type MilestonePage struct {
	Milestones     []Milestone `json:"milestones"`
	Page           int         `json:"page"`
	PerPage        int         `json:"perPage"`
	HasNext        bool        `json:"hasNext"`
	ViewerCanWrite bool        `json:"viewerCanWrite"`
}

type CreateInput struct {
	Title       string
	Description string
	DueOn       *string
}

type UpdateInput struct {
	Title           *string
	Description     *string
	State           *string
	DueOn           *string
	DueOnSet        bool
	ExpectedVersion int64
}

type Store interface {
	List(context.Context, string, string, int, int) (MilestonePage, error)
	Get(context.Context, string, int64) (Milestone, error)
	Create(context.Context, platform.User, RepositoryRef, CreateInput) (Milestone, error)
	Update(context.Context, platform.User, RepositoryRef, int64, UpdateInput) (Milestone, error)
	Delete(context.Context, platform.User, RepositoryRef, int64, int64) error
	AssignIssue(
		context.Context, platform.User, RepositoryRef, int64, int64,
	) (collab.MilestoneSummary, error)
	RemoveIssue(context.Context, platform.User, RepositoryRef, int64) error
}
