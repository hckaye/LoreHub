package cmdutil

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type searchResults struct {
	Repositories  []repository         `json:"repositories"`
	Organizations []searchOrganization `json:"organizations"`
	Users         []searchUser         `json:"users"`
	Issues        []searchWorkItem     `json:"issues"`
	PullRequests  []searchWorkItem     `json:"pullRequests"`
	Counts        searchCounts         `json:"counts"`
	Page          int                  `json:"page"`
	PerPage       int                  `json:"perPage"`
}

type searchOrganization struct {
	ID                          string    `json:"id"`
	Slug                        string    `json:"slug"`
	DisplayName                 string    `json:"displayName"`
	Description                 string    `json:"description"`
	Visibility                  string    `json:"visibility"`
	WebsiteURL                  string    `json:"websiteUrl"`
	ContactEmail                string    `json:"contactEmail"`
	DefaultRepositoryVisibility string    `json:"defaultRepositoryVisibility"`
	Role                        string    `json:"role"`
	MemberCount                 int64     `json:"memberCount"`
	RepositoryCount             int64     `json:"repositoryCount"`
	TeamCount                   int64     `json:"teamCount"`
	CreatedAt                   time.Time `json:"createdAt"`
}

type searchUser struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	AvatarURL   string `json:"avatarUrl"`
}

type searchWorkItem struct {
	ID            string           `json:"id"`
	Kind          string           `json:"kind"`
	Repository    searchRepository `json:"repository"`
	Number        int64            `json:"number"`
	Title         string           `json:"title"`
	State         string           `json:"state"`
	IsDraft       bool             `json:"isDraft"`
	Author        searchUser       `json:"author"`
	Assignees     []searchUser     `json:"assignees"`
	Labels        []searchLabel    `json:"labels"`
	Milestone     *searchMilestone `json:"milestone"`
	CommentCount  int64            `json:"commentCount"`
	ApprovalCount int64            `json:"approvalCount"`
	SourceBranch  string           `json:"sourceBranch,omitempty"`
	TargetBranch  string           `json:"targetBranch,omitempty"`
	CreatedAt     time.Time        `json:"createdAt"`
	UpdatedAt     time.Time        `json:"updatedAt"`
}

type searchRepository struct {
	ID          string `json:"id"`
	Owner       string `json:"owner"`
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
}

type searchLabel struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type searchMilestone struct {
	Number int64  `json:"number"`
	Title  string `json:"title"`
}

type searchCounts struct {
	Repositories  int64 `json:"repositories"`
	Organizations int64 `json:"organizations"`
	Users         int64 `json:"users"`
	Issues        int64 `json:"issues"`
	PullRequests  int64 `json:"pullRequests"`
}

func newSearchCommand(state *rootState) *cobra.Command {
	searchCommand := &cobra.Command{
		Use:   "search",
		Short: "Search LoreHub",
	}
	searchCommand.AddCommand(
		newSearchKindCommand(state, "repos", "repositories"),
		newSearchKindCommand(state, "issues", "issues"),
		newSearchKindCommand(state, "prs", "pulls"),
	)
	return searchCommand
}

func newSearchKindCommand(state *rootState, name string, searchType string) *cobra.Command {
	return &cobra.Command{
		Use:   name + " QUERY",
		Short: "Search " + name,
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			query := strings.TrimSpace(args[0])
			if query == "" {
				return fmt.Errorf("search query is required")
			}
			client, err := state.clientForHost(state.commandHost())
			if err != nil {
				return err
			}
			values := url.Values{}
			values.Set("q", query)
			values.Set("type", searchType)
			var response searchResults
			if err := getJSON(command.Context(), client,
				queryPath("/api/v1/search", values), &response); err != nil {
				return statusError(command, "search "+name, err)
			}
			return writeResource(command, state.json, response,
				searchHeaders(name), searchRows(name, response))
		},
	}
}

func searchHeaders(kind string) []string {
	if kind == "repos" {
		return []string{"OWNER", "NAME", "DISPLAY NAME", "VISIBILITY"}
	}
	return []string{"REPOSITORY", "NUMBER", "TITLE", "STATE", "AUTHOR"}
}

func searchRows(kind string, response searchResults) [][]string {
	if kind == "repos" {
		rows := make([][]string, 0, len(response.Repositories))
		for _, item := range response.Repositories {
			rows = append(rows, []string{item.Owner, item.Slug, item.DisplayName, item.Visibility})
		}
		return rows
	}
	items := response.Issues
	if kind == "prs" {
		items = response.PullRequests
	}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{
			item.Repository.Owner + "/" + item.Repository.Slug,
			strconv.FormatInt(item.Number, 10), item.Title, item.State, searchAuthor(item.Author),
		})
	}
	return rows
}

func searchAuthor(author searchUser) string {
	if author.Username != "" {
		return author.Username
	}
	return author.DisplayName
}
