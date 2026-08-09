package platform

import "time"

type User struct {
	ID          string
	Username    string
	DisplayName string
	Email       string
	Locale      string
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
	ID                string    `json:"id"`
	OrganizationID    string    `json:"organizationId"`
	Owner             string    `json:"owner"`
	Slug              string    `json:"slug"`
	DisplayName       string    `json:"displayName"`
	Description       string    `json:"description"`
	Visibility        string    `json:"visibility"`
	LoreRepositoryID  string    `json:"loreRepositoryId"`
	LoreURL           string    `json:"loreUrl"`
	DefaultBranch     string    `json:"defaultBranch"`
	IssueCount        int64     `json:"issueCount"`
	MergeRequestCount int64     `json:"mergeRequestCount"`
	UpdatedAt         time.Time `json:"updatedAt"`
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
	DefaultBranch    string
}

type Team struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	Organization   string    `json:"organization"`
	Slug           string    `json:"slug"`
	DisplayName    string    `json:"displayName"`
	Description    string    `json:"description"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type TeamMember struct {
	TeamID      string    `json:"teamId"`
	UserID      string    `json:"userId"`
	Username    string    `json:"username"`
	DisplayName string    `json:"displayName"`
	Role        string    `json:"role"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"createdAt"`
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

type SetRepositoryPolicyInput struct {
	AllowCrossRepositoryLinks bool `json:"allowCrossRepositoryLinks"`
	ObliterateEnabled         bool `json:"obliterateEnabled"`
}

type RepositoryPolicy struct {
	AllowCrossRepositoryLinks bool `json:"allowCrossRepositoryLinks"`
	ObliterateEnabled         bool `json:"obliterateEnabled"`
}

type MergeAuthorizationInput struct {
	RepositoryID string
	BranchID     string
	BranchName   string
	ExpectedBase string
	ExpectedHead string
	Lifetime     time.Duration
}

type CreateIssueInput struct {
	Title string
	Body  string
}

type CreateMergeRequestInput struct {
	Title          string
	Body           string
	SourceBranch   string
	TargetBranch   string
	SourceRevision string
	TargetRevision string
}
