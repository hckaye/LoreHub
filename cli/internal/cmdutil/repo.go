package cmdutil

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/lorehub/lorehub/cli/internal/config"
	"github.com/lorehub/lorehub/cli/internal/text"
	"github.com/spf13/cobra"
)

func newRepoCommand(state *rootState) *cobra.Command {
	repo := &cobra.Command{
		Use:   "repo",
		Short: "Manage repositories",
	}
	repo.AddCommand(
		newRepoListCommand(state),
		newRepoViewCommand(state),
		newRepoCreateCommand(state),
		newRepoCloneCommand(state),
		newRepoSetDefaultCommand(state),
	)
	return repo
}

func newRepoCloneCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "clone OWNER/NAME [DIR]",
		Short: "Clone a Lore repository",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(command *cobra.Command, args []string) error {
			if _, err := exec.LookPath("lore"); err != nil {
				return fmt.Errorf("lore CLI is not installed; install the Lore CLI and ensure lore is on PATH")
			}
			repoContext, err := ParseRepoContext(args[0])
			if err != nil || repoContext.Host != "" {
				return fmt.Errorf("repository must be OWNER/NAME")
			}
			repoContext.Host = state.commandHost()
			client, err := state.clientForRepo(repoContext)
			if err != nil {
				return err
			}

			var metadata repository
			if err := getJSON(command.Context(), client, methodPath(repoContext, ""), &metadata); err != nil {
				return statusError(command, "get repository", err)
			}
			if strings.TrimSpace(metadata.LoreURL) == "" {
				return fmt.Errorf("repository %s has no Lore URL", repoContext)
			}

			var account accountResponse
			if err := getJSON(command.Context(), client, "/api/v1/account", &account); err != nil {
				return statusError(command, "check token permissions", err)
			}
			if err := checkClonePermissions(command, account); err != nil {
				return err
			}

			if err := runLoreAuth(command.Context(), client.Token, metadata.LoreURL); err != nil {
				return err
			}
			if err := runLoreClone(command.Context(), metadata.LoreURL, args[1:]); err != nil {
				return err
			}
			if state.json {
				return state.writeJSON(map[string]any{
					"repository": repoContext.String(),
					"loreUrl":    metadata.LoreURL,
					"cloned":     true,
				})
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Cloned %s\n", repoContext.String())
			return err
		},
	}
}

func checkClonePermissions(command *cobra.Command, account accountResponse) error {
	if account.Token == nil {
		return fmt.Errorf("check token permissions: API did not report token permissions")
	}
	permissions := make(map[string]bool, len(account.Token.Permissions))
	for _, permission := range account.Token.Permissions {
		permissions[permission] = true
	}
	missing := make([]string, 0, 2)
	if !permissions["api"] && !permissions["read_api"] {
		missing = append(missing, "read_api or api")
	}
	if !permissions["read_repository"] && !permissions["write_repository"] {
		missing = append(missing, "read_repository or write_repository")
	}
	if len(missing) == 0 {
		return nil
	}
	message := "token is missing " + strings.Join(missing, "; ")
	_, _ = fmt.Fprintf(command.ErrOrStderr(), "Warning: %s\n", message)
	return fmt.Errorf("cannot clone: %s", message)
}

func runLoreAuth(ctx context.Context, token string, loreURL string) error {
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("cannot authenticate lore CLI: token is empty")
	}
	arguments := []string{
		"auth", "login", "--token-type", "api-key", "--token", token, loreURL,
	}
	process := exec.CommandContext(ctx, "lore", arguments...)
	process.Env = append(os.Environ(), "LOREHUB_TOKEN="+token)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	process.Stdout = &stdout
	process.Stderr = &stderr
	if err := process.Run(); err != nil {
		return fmt.Errorf("lore auth login failed")
	}
	return nil
}

func runLoreClone(ctx context.Context, loreURL string, directory []string) error {
	arguments := []string{"clone", loreURL}
	arguments = append(arguments, directory...)
	process := exec.CommandContext(ctx, "lore", arguments...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	process.Stdout = &stdout
	process.Stderr = &stderr
	if err := process.Run(); err != nil {
		return fmt.Errorf("lore clone failed")
	}
	return nil
}

func newRepoListCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "list [OWNER]",
		Short: "List repositories owned by a user or organization",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			host := state.commandHost()
			client, err := state.clientForHost(host)
			if err != nil {
				return err
			}
			owner := ""
			if len(args) == 1 {
				owner = strings.TrimSpace(args[0])
				if owner == "" || strings.ContainsAny(owner, "/ \t\r\n") {
					return fmt.Errorf("owner is invalid")
				}
			} else {
				var account accountResponse
				if err := getJSON(command.Context(), client, "/api/v1/account", &account); err != nil {
					return statusError(command, "get account", err)
				}
				owner = account.User.Username
				if owner == "" {
					return fmt.Errorf("account response did not include a username")
				}
			}

			var response repositoryListResponse
			path := "/api/v1/organizations/" + url.PathEscape(owner) + "/repositories"
			if len(args) == 0 {
				path = "/api/v1/users/" + url.PathEscape(owner) + "/repositories"
			}
			err = getJSON(command.Context(), client, path, &response)
			if err != nil {
				return statusError(command, "list repositories", err)
			}
			rows := make([][]string, 0, len(response.Repositories))
			for _, repository := range response.Repositories {
				rows = append(rows, []string{repository.Owner, repository.Slug, repository.DisplayName,
					repository.Visibility, strconv.FormatInt(repository.IssueCount, 10),
					strconv.FormatInt(repository.MergeRequestCount, 10)})
			}
			return writeResource(command, state.json, response,
				[]string{"OWNER", "NAME", "DISPLAY NAME", "VISIBILITY", "ISSUES", "PRS"}, rows)
		},
	}
}

func newRepoViewCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "view [OWNER/NAME]",
		Short: "View a repository",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			repoContext, err := state.resolveRepoArgument(args)
			if err != nil {
				return err
			}
			client, err := state.clientForRepo(repoContext)
			if err != nil {
				return err
			}
			var response repository
			if err := getJSON(command.Context(), client, methodPath(repoContext, ""), &response); err != nil {
				return statusError(command, "get repository", err)
			}
			rows := [][]string{
				{"Name", response.Owner + "/" + response.Slug},
				{"Description", response.Description},
				{"Visibility", response.Visibility},
				{"Default branch", response.DefaultBranch},
				{"Issues", strconv.FormatInt(response.IssueCount, 10)},
				{"Pull requests", strconv.FormatInt(response.MergeRequestCount, 10)},
			}
			return writeResource(command, state.json, response, []string{"Field", "Value"}, rows)
		},
	}
}

func newRepoCreateCommand(state *rootState) *cobra.Command {
	var visibility string
	var description string
	command := &cobra.Command{
		Use:   "create ORG/NAME",
		Short: "Create a repository in an organization",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			repoContext, err := ParseRepoContext(args[0])
			if err != nil || repoContext.Host != "" {
				return fmt.Errorf("repository must be ORG/NAME")
			}
			client, err := state.clientForHost(state.commandHost())
			if err != nil {
				return err
			}
			var response repository
			input := struct {
				Slug        string `json:"slug"`
				Description string `json:"description"`
				Visibility  string `json:"visibility"`
			}{Slug: repoContext.Name, Description: description, Visibility: visibility}
			path := "/api/v1/organizations/" + url.PathEscape(repoContext.Owner) + "/repositories"
			if err := postJSON(command.Context(), client, path, input, &response); err != nil {
				return statusError(command, "create repository", err)
			}
			if state.json {
				return state.writeJSON(response)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Created repository %s/%s\n", response.Owner, response.Slug)
			return err
		},
	}
	command.Flags().StringVar(&visibility, "visibility", "private", "repository visibility (private, internal, or public)")
	command.Flags().StringVar(&description, "description", "", "repository description")
	return command
}

func newRepoSetDefaultCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "set-default OWNER/NAME",
		Short: "Set the default repository for a host",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			repository, err := config.ParseRepo(args[0])
			if err != nil {
				return err
			}
			host := state.commandHost()
			hosts, err := state.loadHosts()
			if err != nil {
				return err
			}
			key := state.selectedHostKey(hosts, host)
			entry, _ := state.selectedHostEntry(hosts, host)
			entry.DefaultRepo = repository
			hosts[key] = entry
			if err := state.config.Save(hosts); err != nil {
				return err
			}

			result := map[string]string{"host": host, "defaultRepo": repository}
			if state.json {
				return text.NewWriter(command.OutOrStdout()).JSON(result)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Default repository for %s set to %s\n", host, repository)
			return err
		},
	}
}

func (state *rootState) resolveRepoArgument(args []string) (RepoContext, error) {
	if len(args) == 0 {
		return state.resolveRepo()
	}
	repository, err := ParseRepoContext(args[0])
	if err != nil {
		return RepoContext{}, err
	}
	if repository.Host == "" {
		repository.Host = state.host()
	}
	return repository, nil
}
