package platform

import "time"

type User struct {
	ID          string
	Username    string
	DisplayName string
	AvatarURL   string
	Email       string
	Locale      string
}

type AuditActor struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

type AuditRepository struct {
	ID    string `json:"id"`
	Owner string `json:"owner"`
	Slug  string `json:"slug"`
}

type AuditEvent struct {
	ID            string           `json:"id"`
	Action        string           `json:"action"`
	TargetType    string           `json:"targetType"`
	TargetID      *string          `json:"targetId"`
	Actor         *AuditActor      `json:"actor"`
	Repository    *AuditRepository `json:"repository"`
	RemoteAddress *string          `json:"remoteAddress"`
	Details       map[string]any   `json:"details"`
	OccurredAt    time.Time        `json:"occurredAt"`
}

type AuditLogPage struct {
	Items      []AuditEvent `json:"items"`
	NextCursor *string      `json:"nextCursor"`
}

type Organization struct {
	ID          string    `json:"id"`
	Slug        string    `json:"slug"`
	DisplayName string    `json:"displayName"`
	Description string    `json:"description"`
	Visibility  string    `json:"visibility"`
	CreatedAt   time.Time `json:"createdAt"`
}

type Repository struct {
	ID                 string     `json:"id"`
	OrganizationID     string     `json:"organizationId"`
	Owner              string     `json:"owner"`
	Slug               string     `json:"slug"`
	DisplayName        string     `json:"displayName"`
	Description        string     `json:"description"`
	Visibility         string     `json:"visibility"`
	LoreRepositoryID   string     `json:"loreRepositoryId"`
	LoreURL            string     `json:"loreUrl"`
	LoreServerID       string     `json:"loreServerId"`
	DefaultBranch      string     `json:"defaultBranch"`
	HomepageURL        string     `json:"homepageUrl"`
	AllowIssues        bool       `json:"allowIssues"`
	AllowMergeRequests bool       `json:"allowMergeRequests"`
	Topics             []string   `json:"topics"`
	IssueCount         int64      `json:"issueCount"`
	MergeRequestCount  int64      `json:"mergeRequestCount"`
	ArchivedAt         *time.Time `json:"archivedAt"`
	MigratingAt        *time.Time `json:"migratingAt,omitempty"`
	LifecycleState     string     `json:"lifecycleState,omitempty"`
	ProvisioningError  string     `json:"provisioningError,omitempty"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}

const (
	RepositoryMigrationPending    = "pending"
	RepositoryMigrationMirroring  = "mirroring"
	RepositoryMigrationRepointing = "repointing"
	RepositoryMigrationCompleted  = "completed"
	RepositoryMigrationFailed     = "failed"
)

type RepositoryMigration struct {
	ID           string     `json:"id"`
	RepositoryID string     `json:"repositoryId"`
	FromServerID string     `json:"fromServerId"`
	ToServerID   string     `json:"toServerId"`
	State        string     `json:"state"`
	ErrorText    string     `json:"errorText,omitempty"`
	CreatedBy    string     `json:"createdBy"`
	CreatedAt    time.Time  `json:"createdAt"`
	StartedAt    *time.Time `json:"startedAt,omitempty"`
	CompletedAt  *time.Time `json:"completedAt,omitempty"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

type Issue struct {
	ID           string    `json:"id"`
	Number       int64     `json:"number"`
	Title        string    `json:"title"`
	Body         string    `json:"body"`
	State        string    `json:"state"`
	Author       string    `json:"author"`
	Assignee     *string   `json:"assignee"`
	CommentCount int64     `json:"commentCount"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type MergeRequest struct {
	ID             string    `json:"id"`
	Number         int64     `json:"number"`
	Title          string    `json:"title"`
	Body           string    `json:"body"`
	State          string    `json:"state"`
	IsDraft        bool      `json:"isDraft"`
	SourceBranch   string    `json:"sourceBranch"`
	TargetBranch   string    `json:"targetBranch"`
	SourceRevision string    `json:"sourceRevision"`
	TargetRevision string    `json:"targetRevision"`
	Author         string    `json:"author"`
	ApprovalCount  int64     `json:"approvalCount"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type CIRun struct {
	ID          string     `json:"id"`
	RunNumber   int64      `json:"runNumber"`
	EventName   string     `json:"eventName"`
	Branch      string     `json:"branch"`
	Revision    string     `json:"revision"`
	Status      string     `json:"status"`
	Conclusion  *string    `json:"conclusion"`
	QueuedAt    time.Time  `json:"queuedAt"`
	StartedAt   *time.Time `json:"startedAt"`
	CompletedAt *time.Time `json:"completedAt"`
}

type CreateOrganizationInput struct {
	Slug        string
	DisplayName string
	Description string
	Visibility  string
}

type RegisterRepositoryInput struct {
	Slug             string
	DisplayName      string
	Description      string
	Visibility       string
	LoreRepositoryID string
	LoreURL          string
	LoreServerID     string
	DefaultBranch    string
}

type ProvisionRepositoryInput struct {
	Slug          string
	DisplayName   string
	Description   string
	Visibility    string
	DefaultBranch string
}

type Team struct {
	ID               string    `json:"id"`
	OrganizationID   string    `json:"organizationId"`
	Organization     string    `json:"organization,omitempty"`
	OrganizationSlug string    `json:"organizationSlug,omitempty"`
	Slug             string    `json:"slug"`
	DisplayName      string    `json:"displayName"`
	Description      string    `json:"description"`
	ViewerRole       string    `json:"viewerRole,omitempty"`
	MemberCount      int64     `json:"memberCount,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type TeamMember struct {
	TeamID      string    `json:"teamId"`
	UserID      string    `json:"userId"`
	Username    string    `json:"username"`
	DisplayName string    `json:"displayName"`
	Role        string    `json:"role"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"createdAt"`
	JoinedAt    time.Time `json:"joinedAt,omitempty"`
}

type TeamRepositoryRole struct {
	TeamID       string    `json:"teamId"`
	RepositoryID string    `json:"repositoryId"`
	Owner        string    `json:"owner"`
	Repository   string    `json:"repository"`
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type Collaborator struct {
	UserID      string `json:"userId"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
	Active      bool   `json:"active"`
	Source      string `json:"source"`
}

type RepositoryInvitation struct {
	ID                    string     `json:"id"`
	OrganizationID        string     `json:"organizationId"`
	RepositoryID          string     `json:"repositoryId"`
	Owner                 string     `json:"owner"`
	Repository            string     `json:"repository"`
	RepositoryDisplayName string     `json:"repositoryDisplayName"`
	InviteeUserID         string     `json:"inviteeUserId"`
	InviteeUsername       string     `json:"inviteeUsername"`
	InviteeDisplayName    string     `json:"inviteeDisplayName"`
	InvitedByUserID       string     `json:"invitedByUserId"`
	InvitedByUsername     string     `json:"invitedByUsername"`
	InvitedByDisplayName  string     `json:"invitedByDisplayName"`
	Role                  string     `json:"role"`
	Status                string     `json:"status"`
	ExpiresAt             time.Time  `json:"expiresAt"`
	RespondedAt           *time.Time `json:"respondedAt"`
	CreatedAt             time.Time  `json:"createdAt"`
	UpdatedAt             time.Time  `json:"updatedAt"`
}

type RepositoryInvitationPage struct {
	Invitations []RepositoryInvitation `json:"invitations"`
	Total       int64                  `json:"total"`
	Page        int                    `json:"page"`
	PerPage     int                    `json:"perPage"`
}

type OrganizationMember struct {
	UserID      string    `json:"userId"`
	Username    string    `json:"username"`
	DisplayName string    `json:"displayName"`
	Role        string    `json:"role"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"createdAt"`
}

type RepositoryLink struct {
	ID                 string    `json:"id"`
	SourceRepositoryID string    `json:"sourceRepositoryId"`
	SourceRepository   string    `json:"sourceRepository"`
	TargetRepositoryID string    `json:"targetRepositoryId"`
	TargetRepository   string    `json:"targetRepository"`
	Kind               string    `json:"kind"`
	CreatedAt          time.Time `json:"createdAt"`
}

type SetTeamInput struct {
	Slug        string
	DisplayName string
	Description string
}

type SetOrganizationMemberInput struct {
	Username string
	Role     string
	Active   bool
}

type SetTeamMemberInput struct {
	Username string
	Role     string
	Active   bool
}

type SetTeamRepositoryRoleInput struct {
	Role string
}

type SetCollaboratorInput struct {
	Username string
	Role     string
	Active   bool
}

type CreateRepositoryInvitationInput struct {
	Username string
	Role     string
}

type SetRepositoryPolicyInput struct {
	AllowCrossRepositoryLinks bool `json:"allowCrossRepositoryLinks"`
	ObliterateEnabled         bool `json:"obliterateEnabled"`
}

type RepositoryPolicy struct {
	AllowCrossRepositoryLinks bool `json:"allowCrossRepositoryLinks"`
	ObliterateEnabled         bool `json:"obliterateEnabled"`
}

type MergeAuthorizationInput struct {
	OperationID    string
	RepositoryID   string
	BranchID       string
	BranchName     string
	ExpectedBase   string
	ExpectedHead   string
	SourceRevision string
	Lifetime       time.Duration
}

type CreateIssueInput struct {
	Title string
	Body  string
}

type CreateMergeRequestInput struct {
	Title          string
	Body           string
	IsDraft        bool
	SourceBranch   string
	TargetBranch   string
	SourceRevision string
	TargetRevision string
}
