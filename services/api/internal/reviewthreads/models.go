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
	Pending         bool       `json:"pending"`
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
	PendingReviewID      string
}

// PendingReview is a review that batches inline comments until its author
// submits a verdict. Only the author sees the comments it holds.
type PendingReview struct {
	ID           string    `json:"id"`
	Author       string    `json:"author"`
	Body         string    `json:"body"`
	CommentCount int       `json:"commentCount"`
	CreatedAt    time.Time `json:"createdAt"`
}

// SubmitInput publishes a pending review. Body overrides the stored draft body
// when supplied, so the review form can submit its text in a single request.
type SubmitInput struct {
	Decision string
	Body     *string
}

// SubmitResult reports the verdict recorded on the merge request and how many
// batched comments became visible to everyone.
type SubmitResult struct {
	ReviewID          string    `json:"reviewId"`
	Decision          string    `json:"decision"`
	Body              string    `json:"body"`
	PublishedComments int       `json:"publishedComments"`
	SubmittedAt       time.Time `json:"submittedAt"`
}

type Store interface {
	List(context.Context, string, int64, string) ([]Thread, error)
	Create(context.Context, platform.User, RepositoryRef, int64, CreateInput) (Thread, error)
	Reply(context.Context, platform.User, RepositoryRef, int64, string, string, string) (Comment, error)
	UpdateComment(
		context.Context, platform.User, RepositoryRef, int64, string, string, string, int,
	) (Comment, error)
	DeleteComment(context.Context, platform.User, RepositoryRef, int64, string, string, int) error
	SetResolved(context.Context, platform.User, RepositoryRef, int64, string, bool, int) (Thread, error)
	PendingReview(context.Context, string, int64, string) (*PendingReview, error)
	StartPendingReview(context.Context, platform.User, RepositoryRef, int64) (PendingReview, bool, error)
	UpdatePendingReview(context.Context, platform.User, RepositoryRef, int64, string) (PendingReview, error)
	SubmitPendingReview(
		context.Context, platform.User, RepositoryRef, int64, SubmitInput,
	) (SubmitResult, error)
	DiscardPendingReview(context.Context, platform.User, RepositoryRef, int64) error
}

type ThreadList struct {
	Threads       []Thread       `json:"threads"`
	PendingReview *PendingReview `json:"pendingReview,omitempty"`
}
