package cmdutil

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lorehub/lorehub/cli/internal/api"
	"github.com/lorehub/lorehub/cli/internal/config"
	"github.com/lorehub/lorehub/cli/internal/text"
	"github.com/spf13/cobra"
)

type repository struct {
	ID                 string     `json:"id"`
	OrganizationID     string     `json:"organizationId"`
	Owner              string     `json:"owner"`
	Slug               string     `json:"slug"`
	DisplayName        string     `json:"displayName"`
	Description        string     `json:"description"`
	Visibility         string     `json:"visibility"`
	LoreRepositoryID   string     `json:"loreRepositoryId"`
	LoreURL            string     `json:"loreUrl"`
	DefaultBranch      string     `json:"defaultBranch"`
	HomepageURL        string     `json:"homepageUrl"`
	AllowIssues        bool       `json:"allowIssues"`
	AllowMergeRequests bool       `json:"allowMergeRequests"`
	Topics             []string   `json:"topics"`
	IssueCount         int64      `json:"issueCount"`
	MergeRequestCount  int64      `json:"mergeRequestCount"`
	ArchivedAt         *time.Time `json:"archivedAt"`
	LifecycleState     string     `json:"lifecycleState,omitempty"`
	ProvisioningError  string     `json:"provisioningError,omitempty"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}

type repositoryListResponse struct {
	Repositories []repository `json:"repositories"`
}

type issue struct {
	ID                       string     `json:"id"`
	Number                   int64      `json:"number"`
	Title                    string     `json:"title"`
	Body                     string     `json:"body"`
	State                    string     `json:"state"`
	Author                   string     `json:"author"`
	Assignee                 *string    `json:"assignee"`
	Assignees                []any      `json:"assignees"`
	Labels                   []any      `json:"labels"`
	Milestone                any        `json:"milestone"`
	LabelCount               int64      `json:"labelCount"`
	CommentCount             int64      `json:"commentCount"`
	CreatedAt                time.Time  `json:"createdAt"`
	UpdatedAt                time.Time  `json:"updatedAt"`
	ClosedBy                 *string    `json:"closedBy"`
	ClosedAt                 *time.Time `json:"closedAt"`
	ViewerCanUpdate          bool       `json:"viewerCanUpdate"`
	ViewerCanManageLabels    bool       `json:"viewerCanManageLabels"`
	ViewerCanManageMilestone bool       `json:"viewerCanManageMilestone"`
	ViewerCanManageAssignees bool       `json:"viewerCanManageAssignees"`
}

type issuePage struct {
	Issues      []issue `json:"issues"`
	TotalCount  int64   `json:"totalCount"`
	OpenCount   int64   `json:"openCount"`
	ClosedCount int64   `json:"closedCount"`
	Page        int     `json:"page"`
	PerPage     int     `json:"perPage"`
	HasNext     bool    `json:"hasNext"`
}

type issueComment struct {
	ID              string     `json:"id"`
	IssueID         string     `json:"issueId"`
	Author          string     `json:"author"`
	Body            string     `json:"body"`
	CreatedAt       time.Time  `json:"createdAt"`
	EditedAt        *time.Time `json:"editedAt"`
	ViewerCanUpdate bool       `json:"viewerCanUpdate"`
}

type commentPage struct {
	Items      []issueComment `json:"items"`
	NextCursor string         `json:"nextCursor,omitempty"`
	HasMore    bool           `json:"hasMore"`
	TotalCount *int64         `json:"totalCount,omitempty"`
}

type mergeRequest struct {
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
	ApprovalCount   int64      `json:"approvalCount"`
	MergedBy        *string    `json:"mergedBy"`
	MergedRevision  *string    `json:"mergedRevision"`
	MergedAt        *time.Time `json:"mergedAt"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	ClosedAt        *time.Time `json:"closedAt"`
	ViewerCanUpdate bool       `json:"viewerCanUpdate"`
	ViewerCanReview bool       `json:"viewerCanReview"`
}

type mergeRequestListItem struct {
	mergeRequest
	Labels       []any          `json:"labels"`
	Assignees    []any          `json:"assignees"`
	Milestone    map[string]any `json:"milestone"`
	CommentCount int64          `json:"commentCount"`
}

type mergeRequestPage struct {
	MergeRequests []mergeRequestListItem `json:"mergeRequests"`
	TotalCount    int64                  `json:"totalCount"`
	OpenCount     int64                  `json:"openCount"`
	ClosedCount   int64                  `json:"closedCount"`
	MergedCount   int64                  `json:"mergedCount"`
	Page          int                    `json:"page"`
	PerPage       int                    `json:"perPage"`
	HasNext       bool                   `json:"hasNext"`
}

type mergeBlocker struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

type mergeOperation struct {
	ID             string   `json:"id"`
	State          string   `json:"state"`
	ConflictPaths  []string `json:"conflictPaths"`
	ErrorCode      string   `json:"errorCode,omitempty"`
	ErrorDetail    string   `json:"errorDetail,omitempty"`
	SourceRevision string   `json:"sourceRevision"`
	TargetRevision string   `json:"targetRevision"`
}

type mergeReadiness struct {
	Ready        bool            `json:"ready"`
	CanMerge     bool            `json:"canMerge"`
	Blockers     []mergeBlocker  `json:"blockers"`
	MergeRequest mergeRequest    `json:"mergeRequest"`
	Operation    *mergeOperation `json:"operation,omitempty"`
	SourceStale  bool            `json:"sourceStale"`
	TargetStale  bool            `json:"targetStale"`
}

type issueView struct {
	issue
	Comments commentPage `json:"comments"`
}

func (state *rootState) clientForHost(host string) (*api.Client, error) {
	hosts, err := state.loadHosts()
	if err != nil {
		return nil, err
	}
	entry, _ := state.selectedHostEntry(hosts, host)
	token, _ := config.ResolveToken(entry.Token)
	if token == "" {
		return nil, fmt.Errorf("authentication is required for %s; run lh auth login", host)
	}
	return state.client(host, token)
}

func (state *rootState) clientForRepo(repository RepoContext) (*api.Client, error) {
	if strings.TrimSpace(repository.Host) == "" {
		repository.Host = state.host()
	}
	return state.clientForHost(repository.Host)
}

func (state *rootState) commandHost() string {
	for _, value := range []string{state.repoFlag, os.Getenv("LH_REPO")} {
		if strings.TrimSpace(value) == "" {
			continue
		}
		repository, err := ParseRepoContext(value)
		if err == nil && repository.Host != "" {
			return repository.Host
		}
	}
	hosts, err := state.loadHosts()
	if err == nil {
		return state.repoHost(hosts)
	}
	return state.host()
}

func queryPath(path string, values url.Values) string {
	if len(values) == 0 {
		return path
	}
	return path + "?" + values.Encode()
}

func addStringQuery(values url.Values, key string, value string) {
	if strings.TrimSpace(value) != "" {
		values.Set(key, strings.TrimSpace(value))
	}
}

func writeResource(command *cobra.Command, jsonOutput bool, value any, headers []string, rows [][]string) error {
	writer := text.NewWriter(command.OutOrStdout())
	if jsonOutput {
		return writer.JSON(value)
	}
	return writer.Table(headers, rows)
}

func getJSON(ctx context.Context, client *api.Client, path string, output any) error {
	if err := client.GetJSON(ctx, path, output); err != nil {
		return err
	}
	return nil
}

func patchJSON(ctx context.Context, client *api.Client, path string, input any, output any) error {
	return client.PatchJSON(ctx, path, input, output)
}

func postJSON(ctx context.Context, client *api.Client, path string, input any, output any) error {
	return client.PostJSON(ctx, path, input, output)
}

func methodPath(repo RepoContext, suffix string) string {
	return repo.apiPath(suffix)
}

func checkNumber(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("number is required")
	}
	if _, err := strconv.ParseInt(value, 10, 64); err != nil {
		return "", fmt.Errorf("number must be an integer")
	}
	return value, nil
}

func problemDetail(err error) string {
	if problem, ok := err.(*api.ProblemError); ok && problem.Detail != "" {
		return problem.Detail
	}
	return err.Error()
}

func statusError(command *cobra.Command, action string, err error) error {
	return fmt.Errorf("%s: %s", action, problemDetail(err))
}
