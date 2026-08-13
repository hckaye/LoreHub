package cmdutil

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lorehub/lorehub/cli/internal/api"
	"github.com/spf13/cobra"
)

type label struct {
	ID           string    `json:"id"`
	RepositoryID string    `json:"repositoryId"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	Color        string    `json:"color"`
	CreatedAt    time.Time `json:"createdAt"`
}

type labelPage struct {
	Items          []label `json:"items"`
	NextCursor     string  `json:"nextCursor,omitempty"`
	HasMore        bool    `json:"hasMore"`
	ViewerCanWrite bool    `json:"viewerCanWrite"`
}

func newLabelCommand(state *rootState) *cobra.Command {
	labelCommand := &cobra.Command{
		Use:   "label",
		Short: "Manage repository labels",
	}
	labelCommand.AddCommand(
		newLabelListCommand(state),
		newLabelCreateCommand(state),
		newLabelEditCommand(state),
		newLabelDeleteCommand(state),
	)
	return labelCommand
}

func newLabelEditCommand(state *rootState) *cobra.Command {
	var newName string
	var color string
	var description string
	command := &cobra.Command{
		Use:   "edit NAME",
		Short: "Edit a repository label",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			if name == "" {
				return fmt.Errorf("label name is required")
			}
			if !command.Flags().Changed("name") && !command.Flags().Changed("color") &&
				!command.Flags().Changed("description") {
				return fmt.Errorf("at least one of --name, --color or --description is required")
			}
			repository, err := state.resolveRepo()
			if err != nil {
				return err
			}
			client, err := state.clientForRepo(repository)
			if err != nil {
				return err
			}
			item, err := findLabel(command, client, repository, name)
			if err != nil {
				return err
			}
			if command.Flags().Changed("name") {
				item.Name = newName
			}
			if command.Flags().Changed("color") {
				item.Color = color
			}
			if command.Flags().Changed("description") {
				item.Description = description
			}
			input := struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				Color       string `json:"color"`
			}{Name: item.Name, Description: item.Description, Color: item.Color}
			var response label
			path := methodPath(repository, "/labels/"+url.PathEscape(item.ID))
			if err := patchJSON(command.Context(), client, path, input, &response); err != nil {
				return statusError(command, "edit label", err)
			}
			if state.json {
				return state.writeJSON(response)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Edited label %s\n", response.Name)
			return err
		},
	}
	command.Flags().StringVar(&newName, "name", "", "new label name")
	command.Flags().StringVar(&color, "color", "", "new label color")
	command.Flags().StringVar(&description, "description", "", "new label description")
	return command
}

func newLabelListCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List repository labels",
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
			var response labelPage
			if err := getJSON(command.Context(), client, methodPath(repository, "/labels"), &response); err != nil {
				return statusError(command, "list labels", err)
			}
			rows := make([][]string, 0, len(response.Items))
			for _, item := range response.Items {
				rows = append(rows, []string{item.Name, item.Color, item.Description})
			}
			return writeResource(command, state.json, response,
				[]string{"NAME", "COLOR", "DESCRIPTION"}, rows)
		},
	}
}

func newLabelCreateCommand(state *rootState) *cobra.Command {
	var name string
	var color string
	var description string
	command := &cobra.Command{
		Use:   "create",
		Short: "Create a repository label",
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
			var response label
			input := struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				Color       string `json:"color"`
			}{Name: name, Description: description, Color: color}
			if err := postJSON(command.Context(), client, methodPath(repository, "/labels"), input, &response); err != nil {
				return statusError(command, "create label", err)
			}
			if state.json {
				return state.writeJSON(response)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Created label %s\n", response.Name)
			return err
		},
	}
	command.Flags().StringVar(&name, "name", "", "label name")
	command.Flags().StringVar(&color, "color", "", "label color")
	command.Flags().StringVar(&description, "description", "", "label description")
	_ = command.MarkFlagRequired("name")
	_ = command.MarkFlagRequired("color")
	return command
}

func newLabelDeleteCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "delete NAME",
		Short: "Delete a repository label",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			if name == "" {
				return fmt.Errorf("label name is required")
			}
			repository, err := state.resolveRepo()
			if err != nil {
				return err
			}
			client, err := state.clientForRepo(repository)
			if err != nil {
				return err
			}
			item, err := findLabel(command, client, repository, name)
			if err != nil {
				return err
			}
			response, err := client.Do(command.Context(), http.MethodDelete,
				methodPath(repository, "/labels/"+url.PathEscape(item.ID)), nil, nil)
			if err != nil {
				return statusError(command, "delete label", err)
			}
			if closeErr := response.Body.Close(); closeErr != nil {
				return fmt.Errorf("delete label: close API response: %w", closeErr)
			}
			if state.json {
				return state.writeJSON(map[string]any{"name": item.Name, "id": item.ID, "deleted": true})
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Deleted label %s\n", item.Name)
			return err
		},
	}
}

func findLabel(command *cobra.Command, client *api.Client, repository RepoContext, name string) (label, error) {
	firstPage := true
	cursor := ""
	for {
		values := url.Values{}
		if !firstPage {
			values.Set("cursor", cursor)
			values.Set("limit", "100")
		}
		var page labelPage
		if err := getJSON(command.Context(), client,
			queryPath(methodPath(repository, "/labels"), values), &page); err != nil {
			return label{}, statusError(command, "list labels", err)
		}
		for _, item := range page.Items {
			if item.Name == name {
				return item, nil
			}
		}
		if !page.HasMore || strings.TrimSpace(page.NextCursor) == "" {
			break
		}
		firstPage = false
		cursor = page.NextCursor
	}
	return label{}, fmt.Errorf("label %q was not found", name)
}
