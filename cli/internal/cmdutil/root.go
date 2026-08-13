package cmdutil

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/lorehub/lorehub/cli/internal/api"
	"github.com/lorehub/lorehub/cli/internal/config"
	"github.com/lorehub/lorehub/cli/internal/text"
	"github.com/spf13/cobra"
)

type Options struct {
	Version     string
	In          io.Reader
	Out         io.Writer
	ErrOut      io.Writer
	ConfigPath  string
	HTTPClient  *http.Client
	DefaultHost string
}

type rootState struct {
	version     string
	input       io.Reader
	output      io.Writer
	errorOutput io.Writer
	config      *config.Store
	httpClient  *http.Client
	defaultHost string
	hostFlag    string
	repoFlag    string
	json        bool
}

func NewRootCommand(options Options) *cobra.Command {
	if options.Version == "" {
		options.Version = "dev"
	}
	if options.In == nil {
		options.In = os.Stdin
	}
	if options.Out == nil {
		options.Out = os.Stdout
	}
	if options.ErrOut == nil {
		options.ErrOut = os.Stderr
	}
	if options.ConfigPath == "" {
		options.ConfigPath = config.DefaultPath()
	}
	if options.DefaultHost == "" {
		options.DefaultHost = config.DefaultHost
	}

	state := &rootState{
		version:     options.Version,
		input:       options.In,
		output:      options.Out,
		errorOutput: options.ErrOut,
		config:      config.NewStore(options.ConfigPath),
		httpClient:  options.HTTPClient,
		defaultHost: options.DefaultHost,
	}
	root := &cobra.Command{
		Use:           "lh",
		Short:         "LoreHub command-line client",
		Version:       state.version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetIn(state.input)
	root.SetOut(state.output)
	root.SetErr(state.errorOutput)
	root.SetVersionTemplate("{{.Version}}\n")
	root.PersistentPreRunE = func(_ *cobra.Command, _ []string) error {
		if strings.TrimSpace(state.repoFlag) == "" {
			return nil
		}
		_, err := ParseRepoContext(state.repoFlag)
		return err
	}
	root.PersistentFlags().StringVar(&state.hostFlag, "host", "", "LoreHub host")
	root.PersistentFlags().StringVar(&state.hostFlag, "hostname", "", "LoreHub host")
	root.PersistentFlags().StringVar(&state.repoFlag, "repo", "", "repository in OWNER/NAME form")
	root.PersistentFlags().BoolVar(&state.json, "json", false, "write JSON output")

	root.AddCommand(
		newAuthCommand(state),
		newAPICommand(state),
		newRepoCommand(state),
		newIssueCommand(state),
		newPRCommand(state),
		newVersionCommand(state),
	)
	return root
}

func Execute(version string) {
	command := NewRootCommand(Options{Version: version})
	if err := command.Execute(); err != nil {
		_, _ = fmt.Fprintln(command.ErrOrStderr(), "lh:", err)
		os.Exit(1)
	}
}

func newVersionCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the lh version",
		Args:  cobra.NoArgs,
		Run: func(command *cobra.Command, _ []string) {
			_, _ = fmt.Fprintln(command.OutOrStdout(), state.version)
		},
	}
}

func (state *rootState) host() string {
	return config.ResolveHost(state.hostFlag, state.defaultHost)
}

func (state *rootState) loadHosts() (config.Hosts, error) {
	return state.config.Load()
}

func (state *rootState) client(host string, token string) (*api.Client, error) {
	client, err := api.NewClient(host, token)
	if err != nil {
		return nil, err
	}
	client.HTTPClient = state.httpClient
	return client, nil
}

func (state *rootState) outputWriter() text.Writer {
	return text.NewWriter(state.output)
}

func (state *rootState) writeJSON(value any) error {
	return state.outputWriter().JSON(value)
}

func (state *rootState) selectedHostEntry(hosts config.Hosts, host string) (config.HostConfig, bool) {
	key := state.selectedHostKey(hosts, host)
	entry, ok := hosts[key]
	if ok {
		return entry, true
	}
	return config.HostConfig{}, false
}

func (state *rootState) selectedHostKey(hosts config.Hosts, host string) string {
	key := config.NormalizeHost(host)
	if _, ok := hosts[key]; ok {
		return key
	}
	for configuredHost := range hosts {
		if config.NormalizeHost(configuredHost) == key {
			return configuredHost
		}
	}
	return key
}
