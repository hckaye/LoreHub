package collab

import "errors"

var ErrInvalidRepositoryWorkItemQuery = errors.New("invalid repository work item query")

type RepositoryIssueQuery struct {
	State            string
	Search           string
	Author           string
	Assignee         string
	Labels           []string
	MilestoneNumber  *int64
	WithoutMilestone bool
	Sort             string
	Direction        string
	Page             int
	PerPage          int
}

type RepositoryIssuePage struct {
	Issues      []Issue `json:"issues"`
	TotalCount  int64   `json:"totalCount"`
	OpenCount   int64   `json:"openCount"`
	ClosedCount int64   `json:"closedCount"`
	Page        int     `json:"page"`
	PerPage     int     `json:"perPage"`
	HasNext     bool    `json:"hasNext"`
}

type MergeRequestListItem struct {
	MergeRequest
	Labels       []Label           `json:"labels"`
	Assignees    []Assignee        `json:"assignees"`
	Milestone    *MilestoneSummary `json:"milestone"`
	CommentCount int64             `json:"commentCount"`
}

type RepositoryMergeRequestQuery struct {
	State            string
	Search           string
	Author           string
	Assignee         string
	Labels           []string
	MilestoneNumber  *int64
	WithoutMilestone bool
	SourceBranch     string
	TargetBranch     string
	Draft            *bool
	Sort             string
	Direction        string
	Page             int
	PerPage          int
}

type RepositoryMergeRequestPage struct {
	MergeRequests []MergeRequestListItem `json:"mergeRequests"`
	TotalCount    int64                  `json:"totalCount"`
	OpenCount     int64                  `json:"openCount"`
	ClosedCount   int64                  `json:"closedCount"`
	MergedCount   int64                  `json:"mergedCount"`
	Page          int                    `json:"page"`
	PerPage       int                    `json:"perPage"`
	HasNext       bool                   `json:"hasNext"`
}
