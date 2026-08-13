package cmdutil

import (
	"fmt"
	"strings"

	"github.com/lorehub/lorehub/cli/internal/config"
	"github.com/spf13/cobra"
)

func newConfigCommand(state *rootState) *cobra.Command {
	command := &cobra.Command{
		Use:   "config",
		Short: "View and update lh configuration",
	}
	command.AddCommand(
		newConfigListCommand(state),
		newConfigGetCommand(state),
		newConfigSetCommand(state),
		newConfigUnsetCommand(state),
	)
	return command
}

func newConfigListCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configuration for the selected host",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			values, err := state.configValues()
			if err != nil {
				return err
			}
			if state.json {
				return state.writeJSON(values)
			}
			rows := [][]string{
				{"host", values["host"]},
				{"default-repo", values["default-repo"]},
				{"config-file", values["config-file"]},
				{"token-source", values["token-source"]},
			}
			return writeResource(command, false, values, []string{"KEY", "VALUE"}, rows)
		},
	}
}

func newConfigGetCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "get KEY",
		Short: "Get a configuration value",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			key := strings.TrimSpace(args[0])
			values, err := state.configValues()
			if err != nil {
				return err
			}
			value, ok := values[key]
			if !ok {
				return fmt.Errorf("unknown configuration key %q", key)
			}
			if state.json {
				return state.writeJSON(map[string]string{key: value})
			}
			_, err = fmt.Fprintln(command.OutOrStdout(), value)
			return err
		},
	}
}

func newConfigSetCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "set default-repo OWNER/NAME",
		Short: "Set the default repository for the selected host",
		Args:  cobra.ExactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			if args[0] != "default-repo" {
				return fmt.Errorf("only default-repo can be changed")
			}
			repository, err := config.ParseRepo(args[1])
			if err != nil {
				return err
			}
			hosts, err := state.loadHosts()
			if err != nil {
				return err
			}
			host := state.commandHost()
			key := state.selectedHostKey(hosts, host)
			entry := hosts[key]
			entry.DefaultRepo = repository
			hosts[key] = entry
			if err := state.config.Save(hosts); err != nil {
				return err
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Set default repository for %s to %s\n", host, repository)
			return err
		},
	}
}

func newConfigUnsetCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "unset default-repo",
		Short: "Clear the default repository for the selected host",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if args[0] != "default-repo" {
				return fmt.Errorf("only default-repo can be changed")
			}
			hosts, err := state.loadHosts()
			if err != nil {
				return err
			}
			host := state.commandHost()
			key := state.selectedHostKey(hosts, host)
			entry := hosts[key]
			entry.DefaultRepo = ""
			if entry.Token == "" {
				delete(hosts, key)
			} else {
				hosts[key] = entry
			}
			if err := state.config.Save(hosts); err != nil {
				return err
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Cleared default repository for %s\n", host)
			return err
		},
	}
}

func (state *rootState) configValues() (map[string]string, error) {
	hosts, err := state.loadHosts()
	if err != nil {
		return nil, err
	}
	host := state.commandHost()
	entry, _ := state.selectedHostEntry(hosts, host)
	_, tokenSource := config.ResolveToken(entry.Token)
	if strings.TrimSpace(entry.Token) == "" {
		if _, source := config.ResolveToken(""); source != "environment" {
			tokenSource = "none"
		}
	}
	return map[string]string{
		"host":         host,
		"default-repo": entry.DefaultRepo,
		"config-file":  state.config.Path(),
		"token-source": tokenSource,
	}, nil
}
