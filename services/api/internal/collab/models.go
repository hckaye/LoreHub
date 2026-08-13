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
	ID                string     `json:"id"`
	OrganizationID    string     `json:"organizationId"`
	Owner             string     `json:"owner"`
	Slug              string     `json:"slug"`
	DisplayName       string     `json:"displayName"`
	Description       string     `json:"description"`
	Visibility        string     `json:"visibility"`
	LoreRepositoryID  string     `json:"loreRepositoryId"`
	LoreURL           string     `json:"loreUrl"`
	DefaultBranch     string     `json:"defaultBranch"`
	Topics            []string   `json:"topics"`
	IssueCount        int64      `json:"issueCount"`
	MergeRequestCount int64      `json:"mergeRequestCount"`
	StarCount         int64      `json:"starCount"`
	WatcherCount      int64      `json:"watcherCount"`
	ViewerHasStarred  bool       `json:"viewerHasStarred"`
	ViewerIsWatching  bool       `json:"viewerIsWatching"`
	ArchivedAt        *time.Time `json:"archivedAt"`
	MigratingAt       *time.Time `json:"migratingAt,omitempty"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

type RepositoryEngagement struct {
	StarCount        int64 `json:"starCount"`
	WatcherCount     int64 `json:"watcherCount"`
	ViewerHasStarred bool  `json:"viewerHasStarred"`
	ViewerIsWatching bool  `json:"viewerIsWatching"`
}

// Issue is a single issue record returned by the detail endpoint.
type Issue struct {
	ID                       string            `json:"id"`
	Number                   int64             `json:"number"`
	Title                    string            `json:"title"`
	Body                     string            `json:"body"`
	State                    string            `json:"state"`
	Author                   string            `json:"author"`
	AuthorID                 string            `json:"-"`
	Assignee                 *string           `json:"assignee"`
	Assignees                []Assignee        `json:"assignees"`
	Labels                   []Label           `json:"labels"`
	Milestone                *MilestoneSummary `json:"milestone"`
	LabelCount               int64             `json:"labelCount"`
	CommentCount             int64             `json:"commentCount"`
	Reactions                []Reaction        `json:"reactions"`
	CreatedAt                time.Time         `json:"createdAt"`
	UpdatedAt                time.Time         `json:"updatedAt"`
	ClosedBy                 *string           `json:"closedBy"`
	ClosedAt                 *time.Time        `json:"closedAt"`
	ViewerCanUpdate          bool              `json:"viewerCanUpdate"`
	ViewerCanManageLabels    bool              `json:"viewerCanManageLabels"`
	ViewerCanManageMilestone bool              `json:"viewerCanManageMilestone"`
	ViewerCanManageAssignees bool              `json:"viewerCanManageAssignees"`
}

type Assignee struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	AvatarURL   string `json:"avatarUrl"`
}

type MilestoneSummary struct {
	ID     string  `json:"id"`
	Number int64   `json:"number"`
	Title  string  `json:"title"`
	State  string  `json:"state"`
	DueOn  *string `json:"dueOn"`
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
	ID              string     `json:"id"`
	IssueID         string     `json:"issueId"`
	Author          string     `json:"author"`
	AuthorID        string     `json:"-"`
	Body            string     `json:"body"`
	CreatedAt       time.Time  `json:"createdAt"`
	EditedAt        *time.Time `json:"editedAt"`
	Reactions       []Reaction `json:"reactions"`
	ViewerCanUpdate bool       `json:"viewerCanUpdate"`
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
type MergeRequest struct {
	ID              string     `json:"id"`
	Number          int64      `json:"number"`
	Title           string     `json:"title"`
	Body            string     `json:"body"`
	State           string     `json:"state"`
	IsDraft         bool       `json:"isDraft"`
	SourceBranch    string     `json:"sourceBranch"`
	TargetBranch    string     `json:"targetBranch"`
	SourceRevision  string     `json:"sourceRevision"`
	TargetRevision  string     `json:"targetRevision"`
	Author          string     `json:"author"`
	AuthorID        string     `json:"-"`
	ApprovalCount   int64      `json:"approvalCount"`
	MergedBy        *string    `json:"mergedBy"`
	MergedRevision  *string    `json:"mergedRevision"`
	MergedAt        *time.Time `json:"mergedAt"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	ClosedAt        *time.Time `json:"closedAt"`
	Reactions       []Reaction `json:"reactions"`
	ViewerCanUpdate bool       `json:"viewerCanUpdate"`
	ViewerCanReview bool       `json:"viewerCanReview"`
}

type MergeRequestComment struct {
	ID              string     `json:"id"`
	MergeRequestID  string     `json:"mergeRequestId"`
	Author          string     `json:"author"`
	AuthorID        string     `json:"-"`
	Body            string     `json:"body"`
	CreatedAt       time.Time  `json:"createdAt"`
	EditedAt        *time.Time `json:"editedAt"`
	Reactions       []Reaction `json:"reactions"`
	ViewerCanUpdate bool       `json:"viewerCanUpdate"`
}

// MergeOperation records durable progress through a Lore merge workspace.
// Repository contents are never stored here; only operation metadata is.
type MergeOperation struct {
	ID              string            `json:"id"`
	MergeRequestID  string            `json:"mergeRequestId"`
	RepositoryID    string            `json:"repositoryId"`
	ActorID         string            `json:"-"`
	SourceRevision  string            `json:"sourceRevision"`
	TargetRevision  string            `json:"targetRevision"`
	StagedRevision  string            `json:"stagedRevision,omitempty"`
	PushedRevision  string            `json:"pushedRevision,omitempty"`
	ParentRevisions []string          `json:"parentRevisions"`
	Resolutions     []MergeResolution `json:"resolutions"`
	State           string            `json:"state"`
	ConflictPaths   []string          `json:"conflictPaths"`
	ErrorCode       string            `json:"errorCode,omitempty"`
	ErrorDetail     string            `json:"errorDetail,omitempty"`
	LeaseOwner      string            `json:"-"`
	LeaseExpiresAt  *time.Time        `json:"leaseExpiresAt,omitempty"`
	Version         int64             `json:"version"`
	StartedAt       *time.Time        `json:"startedAt,omitempty"`
	CompletedAt     *time.Time        `json:"completedAt,omitempty"`
	CreatedAt       time.Time         `json:"createdAt"`
	UpdatedAt       time.Time         `json:"updatedAt"`
}

type MergeResolution struct {
	Path      string    `json:"path"`
	Strategy  string    `json:"strategy"`
	Actor     string    `json:"actor,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type MergeBlocker struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

type MergeReadiness struct {
	MergeRequest          MergeRequest          `json:"mergeRequest"`
	CurrentSourceRevision string                `json:"currentSourceRevision"`
	CurrentTargetRevision string                `json:"currentTargetRevision"`
	SourceStale           bool                  `json:"sourceStale"`
	TargetStale           bool                  `json:"targetStale"`
	CanMerge              bool                  `json:"canMerge"`
	Ready                 bool                  `json:"ready"`
	Blockers              []MergeBlocker        `json:"blockers"`
	Reviews               ReviewSummary         `json:"reviews"`
	CISuccess             bool                  `json:"ciSuccess"`
	StatusChecks          []RevisionStatusCheck `json:"statusChecks"`
	DirectPushBlocked     bool                  `json:"directPushBlocked"`
	Rules                 []BranchRule          `json:"rules"`
	Operation             *MergeOperation       `json:"operation,omitempty"`
}

// RevisionStatusCheck is the latest reported state for one status context on
// an exact repository revision. Required is derived from matched branch rules.
type RevisionStatusCheck struct {
	Context     string    `json:"context"`
	State       string    `json:"state"`
	Description string    `json:"description"`
	TargetURL   string    `json:"targetUrl"`
	Creator     string    `json:"creator"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Required    bool      `json:"required"`
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

type ReviewRequest struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	Slug        string    `json:"slug"`
	DisplayName string    `json:"displayName"`
	AvatarURL   string    `json:"avatarUrl,omitempty"`
	Status      string    `json:"status"`
	RequestedBy string    `json:"requestedBy"`
	RequestedAt time.Time `json:"requestedAt"`
}

type ReviewRequestSummary struct {
	Items           []ReviewRequest `json:"items"`
	ViewerCanManage bool            `json:"viewerCanManage"`
}

type ReviewCandidate struct {
	Kind        string `json:"kind"`
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
	AvatarURL   string `json:"avatarUrl,omitempty"`
}

type MergeRequestMetadata struct {
	Labels                   []Label           `json:"labels"`
	Assignees                []Assignee        `json:"assignees"`
	Milestone                *MilestoneSummary `json:"milestone"`
	ViewerCanManageLabels    bool              `json:"viewerCanManageLabels"`
	ViewerCanManageAssignees bool              `json:"viewerCanManageAssignees"`
	ViewerCanManageMilestone bool              `json:"viewerCanManageMilestone"`
}

// BranchRule is a branch protection rule enforced by the Lore merge workflow.
type BranchRule struct {
	ID                   string    `json:"id"`
	RepositoryID         string    `json:"repositoryId"`
	Pattern              string    `json:"pattern"`
	RequiredApprovals    int       `json:"requiredApprovals"`
	RequireCISuccess     bool      `json:"requireCiSuccess"`
	RequiredStatusChecks []string  `json:"requiredStatusChecks"`
	BlockDirectPush      bool      `json:"blockDirectPush"`
	CreatedAt            time.Time `json:"createdAt"`
	UpdatedAt            time.Time `json:"updatedAt"`
}

// BranchRuleInput is the validated payload for creating or updating a rule.
type BranchRuleInput struct {
	Pattern              string   `json:"pattern"`
	RequiredApprovals    int      `json:"requiredApprovals"`
	RequireCISuccess     bool     `json:"requireCiSuccess"`
	RequiredStatusChecks []string `json:"requiredStatusChecks"`
	BlockDirectPush      bool     `json:"blockDirectPush"`
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
	TotalCount *int64 `json:"totalCount,omitempty"`
}
