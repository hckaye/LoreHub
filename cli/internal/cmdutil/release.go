package cmdutil

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lorehub/lorehub/cli/internal/api"
	"github.com/spf13/cobra"
)

type release struct {
	ID             string     `json:"id"`
	TagName        string     `json:"tagName"`
	Title          string     `json:"title"`
	Notes          string     `json:"notes"`
	SourceBranch   string     `json:"sourceBranch"`
	Revision       string     `json:"revision"`
	State          string     `json:"state"`
	CreatedBy      string     `json:"createdBy"`
	PublishedBy    *string    `json:"publishedBy"`
	PublishedAt    *time.Time `json:"publishedAt"`
	Version        int64      `json:"version"`
	Assets         []any      `json:"assets"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	ViewerCanWrite bool       `json:"viewerCanWrite"`
}

type releasePage struct {
	Releases       []release `json:"releases"`
	Page           int       `json:"page"`
	PerPage        int       `json:"perPage"`
	HasNext        bool      `json:"hasNext"`
	ViewerCanWrite bool      `json:"viewerCanWrite"`
}

type repositoryBranches struct {
	Branches []repositoryBranch `json:"branches"`
}

type repositoryBranch struct {
	Name           string `json:"name"`
	LatestRevision string `json:"latestRevision"`
	Archived       bool   `json:"archived"`
}

func newReleaseCommand(state *rootState) *cobra.Command {
	releaseCommand := &cobra.Command{
		Use:   "release",
		Short: "Manage releases",
	}
	releaseCommand.AddCommand(
		newReleaseListCommand(state),
		newReleaseViewCommand(state),
		newReleaseCreateCommand(state),
		newReleaseEditCommand(state),
		newReleaseDeleteCommand(state),
	)
	return releaseCommand
}

func newReleaseListCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List releases",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			repository, err := state.resolveRepo()
			if err != nil {
				return err
			}
			client, err := state.clientForRepo(repository)
			if err != nil {
				return err
			}
			var response releasePage
			if err := getJSON(command.Context(), client, methodPath(repository, "/releases"), &response); err != nil {
				return statusError(command, "list releases", err)
			}
			return writeResource(command, state.json, response, releaseHeaders(), releaseRows(response.Releases))
		},
	}
}

func newReleaseViewCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "view TAG-or-ID",
		Short: "View a release by tag or ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			selector := strings.TrimSpace(args[0])
			if selector == "" {
				return fmt.Errorf("release tag or ID is required")
			}
			repository, err := state.resolveRepo()
			if err != nil {
				return err
			}
			client, err := state.clientForRepo(repository)
			if err != nil {
				return err
			}

			response, err := resolveRelease(command, client, repository, selector)
			if err != nil {
				return err
			}
			return writeResource(command, state.json, response, []string{"Field", "Value"}, releaseDetailRows(response))
		},
	}
}

func newReleaseEditCommand(state *rootState) *cobra.Command {
	var title string
	var notes string
	command := &cobra.Command{
		Use:   "edit TAG-or-ID",
		Short: "Edit a release",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			selector := strings.TrimSpace(args[0])
			if selector == "" {
				return fmt.Errorf("release tag or ID is required")
			}
			if !command.Flags().Changed("title") && !command.Flags().Changed("notes") {
				return fmt.Errorf("at least one of --title or --notes is required")
			}
			repository, err := state.resolveRepo()
			if err != nil {
				return err
			}
			client, err := state.clientForRepo(repository)
			if err != nil {
				return err
			}
			current, err := resolveRelease(command, client, repository, selector)
			if err != nil {
				return err
			}
			input := map[string]any{"expectedVersion": current.Version}
			if command.Flags().Changed("title") {
				input["title"] = title
			}
			if command.Flags().Changed("notes") {
				input["notes"] = notes
			}
			var response release
			path := methodPath(repository, "/releases/"+url.PathEscape(current.ID))
			if err := patchJSON(command.Context(), client, path, input, &response); err != nil {
				return statusError(command, "edit release", err)
			}
			if state.json {
				return state.writeJSON(response)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Edited release %s\n", response.TagName)
			return err
		},
	}
	command.Flags().StringVar(&title, "title", "", "new release title")
	command.Flags().StringVar(&notes, "notes", "", "new release notes")
	return command
}

func newReleaseDeleteCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "delete TAG-or-ID",
		Short: "Delete a release",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			selector := strings.TrimSpace(args[0])
			if selector == "" {
				return fmt.Errorf("release tag or ID is required")
			}
			repository, err := state.resolveRepo()
			if err != nil {
				return err
			}
			client, err := state.clientForRepo(repository)
			if err != nil {
				return err
			}
			current, err := resolveRelease(command, client, repository, selector)
			if err != nil {
				return err
			}
			path := methodPath(repository, "/releases/"+url.PathEscape(current.ID))
			if err := client.DeleteJSON(command.Context(), path,
				map[string]int64{"expectedVersion": current.Version}, nil); err != nil {
				return statusError(command, "delete release", err)
			}
			if state.json {
				return state.writeJSON(map[string]any{
					"id": current.ID, "tagName": current.TagName, "deleted": true,
				})
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Deleted release %s\n", current.TagName)
			return err
		},
	}
}

func resolveRelease(
	command *cobra.Command,
	client *api.Client,
	repository RepoContext,
	selector string,
) (release, error) {
	if !looksLikeUUID(selector) {
		return findReleaseByTag(command, client, repository, selector)
	}
	var response release
	path := methodPath(repository, "/releases/"+url.PathEscape(selector))
	if err := getJSON(command.Context(), client, path, &response); err != nil {
		return release{}, statusError(command, "get release", err)
	}
	return response, nil
}

func newReleaseCreateCommand(state *rootState) *cobra.Command {
	var tag string
	var title string
	var notes string
	var branch string
	command := &cobra.Command{
		Use:   "create",
		Short: "Create a release",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			repository, err := state.resolveRepo()
			if err != nil {
				return err
			}
			client, err := state.clientForRepo(repository)
			if err != nil {
				return err
			}
			var branches repositoryBranches
			if err := getJSON(command.Context(), client, methodPath(repository, "/branches"), &branches); err != nil {
				return statusError(command, "list repository branches", err)
			}
			revision, found := branchRevision(branches.Branches, branch)
			if !found {
				return fmt.Errorf("branch %q was not found or has no revision", branch)
			}
			var response release
			input := struct {
				TagName      string `json:"tagName"`
				Title        string `json:"title"`
				Notes        string `json:"notes"`
				SourceBranch string `json:"sourceBranch"`
				Revision     string `json:"revision"`
			}{TagName: tag, Title: title, Notes: notes, SourceBranch: branch, Revision: revision}
			if err := postJSON(command.Context(), client, methodPath(repository, "/releases"), input, &response); err != nil {
				return statusError(command, "create release", err)
			}
			if state.json {
				return state.writeJSON(response)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Created release %s\n", response.TagName)
			return err
		},
	}
	command.Flags().StringVar(&tag, "tag", "", "release tag")
	command.Flags().StringVar(&title, "title", "", "release title")
	command.Flags().StringVar(&notes, "notes", "", "release notes")
	command.Flags().StringVar(&branch, "branch", "", "source branch")
	_ = command.MarkFlagRequired("tag")
	_ = command.MarkFlagRequired("title")
	_ = command.MarkFlagRequired("branch")
	return command
}

func findReleaseByTag(command *cobra.Command, client *api.Client, repository RepoContext, tag string) (release, error) {
	pageNumber := 1
	perPage := 20
	for {
		values := url.Values{}
		if pageNumber > 1 {
			values.Set("page", strconv.Itoa(pageNumber))
			values.Set("perPage", strconv.Itoa(perPage))
		}
		var page releasePage
		path := queryPath(methodPath(repository, "/releases"), values)
		if err := getJSON(command.Context(), client, path, &page); err != nil {
			return release{}, statusError(command, "list releases", err)
		}
		if page.PerPage > 0 {
			perPage = page.PerPage
		}
		for _, item := range page.Releases {
			if item.TagName == tag {
				return item, nil
			}
		}
		if !page.HasNext {
			break
		}
		pageNumber++
	}
	return release{}, fmt.Errorf("release %q was not found", tag)
}

func branchRevision(branches []repositoryBranch, name string) (string, bool) {
	name = strings.TrimSpace(name)
	for _, branch := range branches {
		if branch.Name == name && !branch.Archived && strings.TrimSpace(branch.LatestRevision) != "" {
			return branch.LatestRevision, true
		}
	}
	return "", false
}

func looksLikeUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		if !((character >= '0' && character <= '9') ||
			(character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')) {
			return false
		}
	}
	return true
}

func releaseHeaders() []string {
	return []string{"TAG", "TITLE", "STATE", "BRANCH", "REVISION"}
}

func releaseRows(releases []release) [][]string {
	rows := make([][]string, 0, len(releases))
	for _, item := range releases {
		rows = append(rows, []string{item.TagName, item.Title, item.State, item.SourceBranch, item.Revision})
	}
	return rows
}

func releaseDetailRows(item release) [][]string {
	return [][]string{
		{"ID", item.ID},
		{"Tag", item.TagName},
		{"Title", item.Title},
		{"State", item.State},
		{"Branch", item.SourceBranch},
		{"Revision", item.Revision},
		{"Notes", item.Notes},
	}
}
