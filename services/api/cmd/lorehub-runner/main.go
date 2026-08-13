package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/runnerclient"
)

const runnerVersion = "0.1.0"

type runnerConfig struct {
	URL      string   `json:"url"`
	Token    string   `json:"token"`
	RunnerID string   `json:"runnerId"`
	Name     string   `json:"name"`
	Labels   []string `json:"labels"`
}

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(arguments []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	if len(arguments) == 0 {
		return errors.New("usage: lorehub-runner <configure|run> [options]")
	}
	switch arguments[0] {
	case "configure":
		return configure(arguments[1:], stdin, stdout, stderr)
	case "run":
		return runDaemon(arguments[1:], stderr)
	default:
		return fmt.Errorf("unknown lorehub-runner command %q", arguments[0])
	}
}

func configure(arguments []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	flags := flag.NewFlagSet("configure", flag.ContinueOnError)
	flags.SetOutput(stderr)
	defaultDirectory, err := defaultConfigDirectory()
	if err != nil {
		return err
	}
	urlValue := flags.String("url", "", "LoreHub URL")
	tokenValue := flags.String("token", "", "runner registration token")
	configDirectory := flags.String("config-dir", defaultDirectory, "runner configuration directory")
	name := flags.String("name", hostname(), "runner name")
	labelsValue := flags.String("labels", "self-hosted,linux,x64", "comma-separated runner labels")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*urlValue) == "" {
		return errors.New("configure requires --url and no positional arguments")
	}
	registrationToken := strings.TrimSpace(*tokenValue)
	if registrationToken != "" {
		_, _ = fmt.Fprintln(stderr, "warning: --token may expose the registration token in the process list; use stdin")
	} else {
		content, err := io.ReadAll(io.LimitReader(stdin, 4096))
		if err != nil {
			return fmt.Errorf("read runner registration token: %w", err)
		}
		registrationToken = strings.TrimSpace(string(content))
	}
	if registrationToken == "" {
		return errors.New("runner registration token is required on stdin")
	}
	labels, err := normalizedLabels(*labelsValue)
	if err != nil {
		return err
	}
	response, err := runnerclient.Register(
		context.Background(), *urlValue, registrationToken, runnerclient.RegistrationRequest{
			Name: strings.TrimSpace(*name), Labels: labels, Version: runnerVersion,
		},
		&http.Client{Timeout: 30 * time.Second},
	)
	if err != nil {
		return fmt.Errorf("register runner: %w", err)
	}
	config := runnerConfig{
		URL: strings.TrimRight(strings.TrimSpace(*urlValue), "/"), Token: response.Token,
		RunnerID: response.Runner.ID, Name: response.Runner.Name, Labels: response.Runner.Labels,
	}
	path, err := writeConfig(*configDirectory, config)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Configured runner %s in %s\n", config.RunnerID, path)
	return nil
}

func runDaemon(arguments []string, stderr io.Writer) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	defaultDirectory, err := defaultConfigDirectory()
	if err != nil {
		return err
	}
	configDirectory := flags.String("config-dir", defaultDirectory, "runner configuration directory")
	workDirectory := flags.String("work-dir", "", "runner work directory")
	loreBinary := flags.String("lore-binary", "lore", "Lore executable")
	actBinary := flags.String("act-binary", "act", "act executable")
	pollPeriod := flags.Duration("poll-period", 2*time.Second, "claim poll period")
	jobTimeout := flags.Duration("job-timeout", 6*time.Hour, "job timeout")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("run does not accept positional arguments")
	}
	config, err := readConfig(*configDirectory)
	if err != nil {
		return err
	}
	if strings.TrimSpace(*workDirectory) == "" {
		*workDirectory = filepath.Join(*configDirectory, "_work")
	}
	client, err := runnerclient.NewClient(config.URL, config.Token, nil)
	if err != nil {
		return err
	}
	executor, err := runnerclient.NewShellExecutor(runnerclient.ShellExecutorConfig{
		Environment: "production", LoreBinary: *loreBinary, ActBinary: *actBinary,
		WorkDirectory: *workDirectory, ServerURL: config.URL,
	})
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewTextHandler(stderr, nil))
	daemon, err := runnerclient.NewDaemon(client, executor, *pollPeriod, *jobTimeout, logger)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err = daemon.Run(ctx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func defaultConfigDirectory() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find runner home directory: %w", err)
	}
	return filepath.Join(home, ".config", "lorehub-runner"), nil
}

func writeConfig(directory string, config runnerConfig) (string, error) {
	directory, err := filepath.Abs(strings.TrimSpace(directory))
	if err != nil || directory == "" {
		return "", errors.New("runner configuration directory is invalid")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("create runner configuration directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return "", fmt.Errorf("restrict runner configuration directory: %w", err)
	}
	content, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode runner configuration: %w", err)
	}
	path := filepath.Join(directory, "config.json")
	temporary, err := os.CreateTemp(directory, ".config-*")
	if err != nil {
		return "", fmt.Errorf("create runner configuration: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}
	if err := temporary.Chmod(0o600); err != nil {
		cleanup()
		return "", fmt.Errorf("restrict runner configuration: %w", err)
	}
	if _, err := temporary.Write(append(content, '\n')); err != nil {
		cleanup()
		return "", fmt.Errorf("write runner configuration: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		cleanup()
		return "", fmt.Errorf("sync runner configuration: %w", err)
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return "", fmt.Errorf("close runner configuration: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		_ = os.Remove(temporaryPath)
		return "", fmt.Errorf("publish runner configuration: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return "", fmt.Errorf("restrict published runner configuration: %w", err)
	}
	return path, nil
}

func readConfig(directory string) (runnerConfig, error) {
	path := filepath.Join(strings.TrimSpace(directory), "config.json")
	info, err := os.Stat(path)
	if err != nil {
		return runnerConfig{}, fmt.Errorf("read runner configuration: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return runnerConfig{}, errors.New("runner configuration must not be accessible by group or other users")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return runnerConfig{}, fmt.Errorf("read runner configuration: %w", err)
	}
	var config runnerConfig
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return runnerConfig{}, fmt.Errorf("decode runner configuration: %w", err)
	}
	if config.URL == "" || config.Token == "" || config.RunnerID == "" {
		return runnerConfig{}, errors.New("runner configuration is incomplete")
	}
	return config, nil
}

func normalizedLabels(value string) ([]string, error) {
	seen := make(map[string]struct{})
	labels := make([]string, 0)
	for _, item := range strings.Split(value, ",") {
		label := strings.ToLower(strings.TrimSpace(item))
		if label == "" || strings.ContainsAny(label, "\x00\r\n\t ") || len(label) > 100 {
			return nil, fmt.Errorf("runner label %q is invalid", item)
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		labels = append(labels, label)
	}
	if _, ok := seen["self-hosted"]; !ok {
		return nil, errors.New("runner labels must include self-hosted")
	}
	sort.Strings(labels)
	return labels, nil
}

func hostname() string {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return "lorehub-runner"
	}
	return strings.TrimSpace(name)
}
