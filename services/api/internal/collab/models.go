// Package collab implements LoreHub collaboration APIs for issues, comments,
// labels, pull-request (merge_request) reviews and branch protection rules.
//
// The package is intentionally self-contained: it owns its HTTP helpers, store
// contracts and route registration so that concurrent auth/HTTP wiring changes
// can merge without conflicts. Handlers depend on the Store interface defined
// here, which makes them trivial to exercise with fakes in unit tests.
package collab

import "time"

// Repository is the collab-facing projection of a repository. It carries the
// identifiers needed by collaboration endpoints without exposing persistence
// details.
type Repository struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	Owner          string    `json:"owner"`
	Slug           string    `json:"slug"`
	Visibility     string    `json:"visibility"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// Issue is a single issue record returned by the detail endpoint.
type Issue struct {
	ID           string     `json:"id"`
	Number       int64      `json:"number"`
	Title        string     `json:"title"`
	Body         string     `json:"body"`
	State        string     `json:"state"`
	Author       string     `json:"author"`
	AuthorID     string     `json:"-"`
	Assignee     *string    `json:"assignee"`
	LabelCount   int64      `json:"labelCount"`
	CommentCount int64      `json:"commentCount"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
	ClosedBy     *string    `json:"closedBy"`
	ClosedAt     *time.Time `json:"closedAt"`
}

// UpdateIssueInput captures the mutable fields of an issue. Pointer fields are
// nil when the client omits them, allowing partial updates.
type UpdateIssueInput struct {
	Title   *string
	Body    *string
	State   *string
	IfMatch *time.Time
}

// IssueComment is a comment on an issue. Author and CreatedAt are immutable;
// EditedAt is non-nil once the comment body has been edited.
type IssueComment struct {
	ID        string     `json:"id"`
	IssueID   string     `json:"issueId"`
	Author    string     `json:"author"`
	AuthorID  string     `json:"-"`
	Body      string     `json:"body"`
	CreatedAt time.Time  `json:"createdAt"`
	EditedAt  *time.Time `json:"editedAt"`
}

// Label is a repository label definition.
type Label struct {
	ID           string    `json:"id"`
	RepositoryID string    `json:"repositoryId"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	Color        string    `json:"color"`
	CreatedAt    time.Time `json:"createdAt"`
}

// LabelInput is the validated payload for creating or updating a label.
type LabelInput struct {
	Name        string
	Description string
	Color       string
}

// MergeRequest is the collab projection of a merge_request (UI "pull request").
// Merge execution is not exposed by this API.
type MergeRequest struct {
	ID             string     `json:"id"`
	Number         int64      `json:"number"`
	Title          string     `json:"title"`
	Body           string     `json:"body"`
	State          string     `json:"state"`
	SourceBranch   string     `json:"sourceBranch"`
	TargetBranch   string     `json:"targetBranch"`
	SourceRevision string     `json:"sourceRevision"`
	TargetRevision string     `json:"targetRevision"`
	Author         string     `json:"author"`
	AuthorID       string     `json:"-"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	ClosedAt       *time.Time `json:"closedAt"`
}

// UpdateMergeRequestInput captures mutable merge_request fields.
type UpdateMergeRequestInput struct {
	Title   *string
	Body    *string
	State   *string
	IfMatch *time.Time
}

// Review is a single reviewer decision bound to a source revision.
type Review struct {
	ID             string    `json:"id"`
	MergeRequestID string    `json:"mergeRequestId"`
	Reviewer       string    `json:"reviewer"`
	ReviewerID     string    `json:"-"`
	SourceRevision string    `json:"sourceRevision"`
	Decision       string    `json:"decision"`
	Body           string    `json:"body"`
	CreatedAt      time.Time `json:"createdAt"`
}

// ReviewSummary aggregates the reviews for a merge request, split by the
// current source revision and the full history.
type ReviewSummary struct {
	CurrentRevision string   `json:"currentRevision"`
	Reviews         []Review `json:"reviews"`
	CurrentReviews  []Review `json:"currentReviews"`
	Approvals       int64    `json:"approvals"`
	ChangeRequests  int64    `json:"changeRequests"`
	Comments        int64    `json:"comments"`
}

// ReviewInput is the validated payload for submitting a review.
type ReviewInput struct {
	Decision string
	Body     string
}

// BranchRule is a branch protection rule. The configuration is descriptive for
// the current product and does not claim enforcement beyond existing behavior.
type BranchRule struct {
	ID                string    `json:"id"`
	RepositoryID      string    `json:"repositoryId"`
	Pattern           string    `json:"pattern"`
	RequiredApprovals int       `json:"requiredApprovals"`
	RequireCISuccess  bool      `json:"requireCiSuccess"`
	BlockDirectPush   bool      `json:"blockDirectPush"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

// BranchRuleInput is the validated payload for creating or updating a rule.
type BranchRuleInput struct {
	Pattern           string
	RequiredApprovals int
	RequireCISuccess  bool
	BlockDirectPush   bool
}

// Page is a bounded pagination window with an opaque string cursor.
type Page struct {
	Limit  int
	Cursor string
}

// Result is a paginated list response.
type Result[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"nextCursor,omitempty"`
	HasMore    bool   `json:"hasMore"`
}
