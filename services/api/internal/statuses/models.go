// Package statuses records external and internal checks for immutable Lore revisions.
package statuses

import (
	"context"
	"errors"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

var ErrInvalidInput = errors.New("invalid revision status input")

type RepositoryRef struct {
	ID             string
	OrganizationID string
}

type Creator struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	AvatarURL   string `json:"avatarUrl"`
}

type Status struct {
	ID          string    `json:"id"`
	Revision    string    `json:"revision"`
	Context     string    `json:"context"`
	State       string    `json:"state"`
	Description string    `json:"description"`
	TargetURL   string    `json:"targetUrl"`
	Creator     Creator   `json:"creator"`
	CreatedAt   time.Time `json:"createdAt"`
}

type Page struct {
	Revision   string   `json:"revision"`
	State      string   `json:"state"`
	Statuses   []Status `json:"statuses"`
	History    []Status `json:"history"`
	Page       int      `json:"page"`
	PerPage    int      `json:"perPage"`
	TotalCount int64    `json:"totalCount"`
	HasNext    bool     `json:"hasNext"`
}

type CreateInput struct {
	Revision       string
	Context        string
	State          string
	Description    string
	TargetURL      string
	IdempotencyKey *string
}

type CreateResult struct {
	Status  Status
	Created bool
}

type Store interface {
	List(context.Context, string, string, int, int) (Page, error)
	Create(context.Context, platform.User, RepositoryRef, CreateInput) (CreateResult, error)
}
