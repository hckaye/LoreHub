package platform

import "time"

type RepositoryInsightCurrent struct {
	OpenIssues        int64 `json:"openIssues"`
	OpenPullRequests  int64 `json:"openPullRequests"`
	Branches          int64 `json:"branches"`
	PublishedReleases int64 `json:"publishedReleases"`
}

type RepositoryInsightPeriod struct {
	IssuesOpened          int64 `json:"issuesOpened"`
	IssuesClosed          int64 `json:"issuesClosed"`
	PullRequestsOpened    int64 `json:"pullRequestsOpened"`
	PullRequestsMerged    int64 `json:"pullRequestsMerged"`
	WorkflowRunsCompleted int64 `json:"workflowRunsCompleted"`
	WorkflowRunsSucceeded int64 `json:"workflowRunsSucceeded"`
	ReleasesPublished     int64 `json:"releasesPublished"`
	BranchPushes          int64 `json:"branchPushes"`
}

type RepositoryInsightDay struct {
	Date                  string `json:"date"`
	IssuesOpened          int64  `json:"issuesOpened"`
	IssuesClosed          int64  `json:"issuesClosed"`
	PullRequestsOpened    int64  `json:"pullRequestsOpened"`
	PullRequestsMerged    int64  `json:"pullRequestsMerged"`
	WorkflowRunsCompleted int64  `json:"workflowRunsCompleted"`
	ReleasesPublished     int64  `json:"releasesPublished"`
	BranchPushes          int64  `json:"branchPushes"`
}

func (day RepositoryInsightDay) ActivityCount() int64 {
	return day.IssuesOpened + day.IssuesClosed + day.PullRequestsOpened + day.PullRequestsMerged +
		day.WorkflowRunsCompleted + day.ReleasesPublished + day.BranchPushes
}

type RepositoryInsightContributor struct {
	ID            string    `json:"id"`
	Username      string    `json:"username"`
	DisplayName   string    `json:"displayName"`
	ActivityCount int64     `json:"activityCount"`
	LastActiveAt  time.Time `json:"lastActiveAt"`
}

type RepositoryInsights struct {
	PeriodDays   int                            `json:"periodDays"`
	PeriodStart  time.Time                      `json:"periodStart"`
	PeriodEnd    time.Time                      `json:"periodEnd"`
	Current      RepositoryInsightCurrent       `json:"current"`
	Period       RepositoryInsightPeriod        `json:"period"`
	Activity     []RepositoryInsightDay         `json:"activity"`
	Contributors []RepositoryInsightContributor `json:"contributors"`
}
