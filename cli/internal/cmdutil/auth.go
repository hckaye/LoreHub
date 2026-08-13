package cmdutil

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/lorehub/lorehub/cli/internal/config"
	"github.com/lorehub/lorehub/cli/internal/text"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newAuthCommand(state *rootState) *cobra.Command {
	auth := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate with LoreHub",
	}
	auth.AddCommand(
		newAuthLoginCommand(state),
		newAuthLogoutCommand(state),
		newAuthStatusCommand(state),
	)
	return auth
}

func newAuthLoginCommand(state *rootState) *cobra.Command {
	var withToken bool
	command := &cobra.Command{
		Use:   "login",
		Short: "Store a personal access token",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			token, err := readToken(command, withToken)
			if err != nil {
				return err
			}
			host := state.host()
			client, err := state.client(host, token)
			if err != nil {
				return err
			}
			var account accountResponse
			if err := client.GetJSON(command.Context(), "/api/v1/account", &account); err != nil {
				return fmt.Errorf("validate token: %w", err)
			}

			hosts, err := state.loadHosts()
			if err != nil {
				return err
			}
			key := state.selectedHostKey(hosts, host)
			entry := hosts[key]
			entry.Token = token
			hosts[key] = entry
			if err := state.config.Save(hosts); err != nil {
				return err
			}

			result := map[string]any{"host": host, "loggedIn": true}
			if state.json {
				return text.NewWriter(command.OutOrStdout()).JSON(result)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Logged in to %s\n", host)
			return err
		},
	}
	command.Flags().BoolVar(&withToken, "with-token", false, "read the token from standard input")
	return command
}

func newAuthLogoutCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the stored personal access token",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			host := state.host()
			hosts, err := state.loadHosts()
			if err != nil {
				return err
			}
			key := state.selectedHostKey(hosts, host)
			entry, found := state.selectedHostEntry(hosts, host)
			if found {
				entry.Token = ""
				if entry.DefaultRepo == "" {
					delete(hosts, key)
				} else {
					hosts[key] = entry
				}
				if err := state.config.Save(hosts); err != nil {
					return err
				}
			}

			environmentToken, _ := config.ResolveToken("")
			result := map[string]any{
				"host":             host,
				"loggedOut":        true,
				"environmentToken": environmentToken != "",
			}
			if state.json {
				return text.NewWriter(command.OutOrStdout()).JSON(result)
			}
			message := fmt.Sprintf("Logged out of %s", host)
			if environmentToken != "" {
				message += "; LH_TOKEN is still set"
			}
			_, err = fmt.Fprintln(command.OutOrStdout(), message)
			return err
		},
	}
}

type authIdentity struct {
	ID          string `json:"id,omitempty"`
	Username    string `json:"username,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	Email       string `json:"email,omitempty"`
}

type accountToken struct {
	ID          string     `json:"id"`
	Prefix      string     `json:"prefix"`
	Permissions []string   `json:"permissions"`
	ExpiresAt   time.Time  `json:"expiresAt"`
	LastUsedAt  *time.Time `json:"lastUsedAt"`
}

type accountResponse struct {
	User  authIdentity  `json:"user"`
	Token *accountToken `json:"token,omitempty"`
}

type authStatus struct {
	Host          string        `json:"host"`
	Authenticated bool          `json:"authenticated"`
	User          *authIdentity `json:"user,omitempty"`
	Permissions   []string      `json:"permissions,omitempty"`
	TokenPrefix   string        `json:"tokenPrefix,omitempty"`
	ExpiresAt     *time.Time    `json:"expiresAt,omitempty"`
	DefaultRepo   string        `json:"defaultRepo,omitempty"`
	TokenSource   string        `json:"tokenSource,omitempty"`
}

func newAuthStatusCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show authentication status",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			host := state.host()
			hosts, err := state.loadHosts()
			if err != nil {
				return err
			}
			entry, _ := state.selectedHostEntry(hosts, host)
			token, source := config.ResolveToken(entry.Token)
			status := authStatus{Host: host, DefaultRepo: entry.DefaultRepo}
			if token != "" {
				client, err := state.client(host, token)
				if err != nil {
					return err
				}
				var account accountResponse
				if err := client.GetJSON(command.Context(), "/api/v1/account", &account); err != nil {
					return fmt.Errorf("check authentication: %w", err)
				}
				status.Authenticated = true
				status.User = &account.User
				if account.Token != nil {
					status.Permissions = account.Token.Permissions
					status.TokenPrefix = account.Token.Prefix
					status.ExpiresAt = &account.Token.ExpiresAt
				}
				status.TokenSource = source
			}

			if state.json {
				return text.NewWriter(command.OutOrStdout()).JSON(status)
			}
			return writeAuthStatus(command, status)
		},
	}
}

func writeAuthStatus(command *cobra.Command, status authStatus) error {
	user := "-"
	if status.User != nil {
		user = status.User.Username
		if user == "" {
			user = status.User.DisplayName
		}
	} else if status.Authenticated {
		user = "not available from API"
	}
	permissions := "not reported by API"
	if !status.Authenticated {
		permissions = "-"
	}
	if len(status.Permissions) > 0 {
		permissions = strings.Join(status.Permissions, ", ")
	}
	authenticated := "no"
	if status.Authenticated {
		authenticated = "yes"
	}
	rows := [][]string{
		{"Host", status.Host},
		{"Authenticated", authenticated},
		{"User", user},
		{"Token prefix", status.TokenPrefix},
		{"Permissions", permissions},
		{"Expires", formatTime(status.ExpiresAt)},
		{"Default repository", status.DefaultRepo},
		{"Token source", status.TokenSource},
	}
	return text.NewWriter(command.OutOrStdout()).Table([]string{"Field", "Value"}, rows)
}

func formatTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return "-"
	}
	return value.UTC().Format(time.RFC3339)
}

func readToken(command *cobra.Command, withToken bool) (string, error) {
	if withToken {
		contents, err := io.ReadAll(io.LimitReader(command.InOrStdin(), 1<<20))
		if err != nil {
			return "", fmt.Errorf("read token: %w", err)
		}
		token := strings.TrimSpace(string(contents))
		if token == "" {
			return "", fmt.Errorf("token is empty")
		}
		return token, nil
	}

	_, _ = fmt.Fprint(command.ErrOrStderr(), "Personal access token: ")
	if file, ok := command.InOrStdin().(*os.File); ok {
		fileDescriptor := int(file.Fd())
		if term.IsTerminal(fileDescriptor) {
			contents, err := term.ReadPassword(fileDescriptor)
			_, _ = fmt.Fprintln(command.ErrOrStderr())
			if err != nil {
				return "", fmt.Errorf("read token: %w", err)
			}
			token := strings.TrimSpace(string(contents))
			if token == "" {
				return "", fmt.Errorf("token is empty")
			}
			return token, nil
		}
	}

	line, err := bufio.NewReader(command.InOrStdin()).ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", fmt.Errorf("read token: %w", err)
	}
	token := strings.TrimSpace(line)
	if token == "" {
		return "", fmt.Errorf("token is empty")
	}
	return token, nil
}
