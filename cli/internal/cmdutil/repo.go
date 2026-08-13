package cmdutil

import (
	"fmt"

	"github.com/lorehub/lorehub/cli/internal/config"
	"github.com/lorehub/lorehub/cli/internal/text"
	"github.com/spf13/cobra"
)

func newRepoCommand(state *rootState) *cobra.Command {
	repo := &cobra.Command{
		Use:   "repo",
		Short: "Manage repositories",
	}
	repo.AddCommand(newRepoSetDefaultCommand(state))
	return repo
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
			host := state.host()
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
