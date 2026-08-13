package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/loresagent"
)

const (
	defaultLoreBuildVersion  = "0.8.6"
	defaultHookModuleVersion = "1.0.0"
)

func main() {
	if err := execute(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "lorehub-lores-agent:", err)
		os.Exit(1)
	}
}

func execute(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("a subcommand is required: configure or run")
	}
	switch args[0] {
	case "configure":
		return configure(args[1:], stdin, stdout, stderr)
	case "run":
		return run(args[1:], stderr)
	case "help", "--help", "-h":
		fmt.Fprintln(stdout, "Usage: lorehub-lores-agent <configure|run> [options]")
		return nil
	default:
		return fmt.Errorf("unknown subcommand %q: expected configure or run", args[0])
	}
}

func configure(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	defaultDir, err := defaultConfigDir()
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("configure", flag.ContinueOnError)
	flags.SetOutput(stderr)
	loreHubURL := flags.String("url", "", "LoreHub base URL")
	loresURL := flags.String("lores-url", "", "advertised lores:// URL")
	name := flags.String("name", "", "Lore Server display name")
	configDir := flags.String("config-dir", defaultDir, "directory for the agent config")
	registrationToken := flags.String("token", "", "registration token; stdin is safer and used by default")
	loreVersion := flags.String("lore-version", "", "Lore build version")
	hookVersion := flags.String("hook-module-version", "", "LoreHub hook module version")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if strings.TrimSpace(*loreHubURL) == "" || strings.TrimSpace(*loresURL) == "" {
		return errors.New("configure requires --url and --lores-url")
	}

	rawToken := strings.TrimSpace(*registrationToken)
	if rawToken != "" {
		fmt.Fprintln(stderr, "warning: --token exposes the registration token in process lists; stdin is safer")
	} else {
		rawToken, err = readRegistrationToken(stdin)
		if err != nil {
			return err
		}
	}
	serverName := strings.TrimSpace(*name)
	if serverName == "" {
		serverName = defaultServerName()
	}
	client, err := loresagent.NewClient(*loreHubURL, nil)
	if err != nil {
		return err
	}
	response, err := client.Register(context.Background(), rawToken, loresagent.RegisterRequest{
		Name:              serverName,
		PublicURL:         strings.TrimSpace(*loresURL),
		LoreBuildVersion:  resolveLoreVersion(*loreVersion),
		HookModuleVersion: resolveHookModuleVersion(*hookVersion),
		HealthMetadata:    map[string]any{"state": "configured"},
	})
	if err != nil {
		if loresagent.IsAuthenticationError(err) {
			return fmt.Errorf("registration token was rejected: %w", err)
		}
		return fmt.Errorf("register Lore Server: %w", err)
	}
	if strings.TrimSpace(response.Credential) == "" {
		return errors.New("register Lore Server: response did not contain a credential")
	}
	config := loresagent.Config{
		LoreHubURL: client.BaseURL,
		Credential: response.Credential,
		ServerID:   response.Server.ID,
		Name:       serverName,
	}
	if err := loresagent.SaveConfig(*configDir, config); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Lore Server registered as %s. Configuration saved to %s.\n",
		serverName, loresagent.ConfigPath(*configDir))
	return nil
}

func run(args []string, stderr io.Writer) error {
	defaultDir, err := defaultConfigDir()
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	interval := flags.Duration("interval", time.Minute, "heartbeat interval")
	configDir := flags.String("config-dir", defaultDir, "directory for the agent config")
	loreVersion := flags.String("lore-version", "", "Lore build version")
	hookVersion := flags.String("hook-module-version", "", "LoreHub hook module version")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *interval <= 0 {
		return errors.New("--interval must be greater than zero")
	}
	config, err := loresagent.LoadConfig(*configDir)
	if err != nil {
		return err
	}
	client, err := loresagent.NewClient(config.LoreHubURL, nil)
	if err != nil {
		return err
	}
	rootContext, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return runAgent(rootContext, client, config, *interval,
		resolveLoreVersion(*loreVersion), resolveHookModuleVersion(*hookVersion), stderr)
}

func runAgent(
	ctx context.Context,
	client *loresagent.Client,
	config loresagent.Config,
	interval time.Duration,
	loreVersion string,
	hookVersion string,
	stderr io.Writer,
) error {
	startedAt := time.Now().UTC()
	sendHeartbeat := func() error {
		uptimeSeconds := int64(time.Since(startedAt).Seconds())
		if uptimeSeconds < 0 {
			uptimeSeconds = 0
		}
		_, err := client.Heartbeat(ctx, config.Credential, loresagent.HeartbeatRequest{
			LoreBuildVersion:  loreVersion,
			HookModuleVersion: hookVersion,
			HealthMetadata: map[string]any{
				"state":         "running",
				"uptimeSeconds": uptimeSeconds,
				"pid":           os.Getpid(),
				"startedAt":     startedAt.Format(time.RFC3339),
			},
		})
		return err
	}

	if err := sendHeartbeat(); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		if loresagent.IsAuthenticationError(err) {
			return fmt.Errorf("authentication failed: LoreHub rejected the server credential: %w", err)
		}
		return fmt.Errorf("initial heartbeat failed: %w", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := sendHeartbeat(); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				if loresagent.IsAuthenticationError(err) {
					return fmt.Errorf("authentication failed: LoreHub rejected the server credential: %w", err)
				}
				fmt.Fprintf(stderr, "heartbeat failed; retrying in %s: %v\n", interval, err)
			}
		}
	}
}

func readRegistrationToken(stdin io.Reader) (string, error) {
	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 1024), 64<<10)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("read registration token from stdin: %w", err)
		}
		return "", errors.New("registration token is required on stdin or with --token")
	}
	token := strings.TrimSpace(scanner.Text())
	if token == "" {
		return "", errors.New("registration token is required on stdin or with --token")
	}
	return token, nil
}

func defaultConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return filepath.Join(home, ".config", "lorehub-lores-agent"), nil
}

func defaultServerName() string {
	if hostname, err := os.Hostname(); err == nil && strings.TrimSpace(hostname) != "" {
		return strings.TrimSpace(hostname)
	}
	return "Lore Server"
}

func resolveLoreVersion(flagValue string) string {
	for _, value := range []string{
		flagValue,
		os.Getenv("LOREHUB_LORE_VERSION"),
		os.Getenv("LOREHUB_LORE_BUILD_VERSION"),
		os.Getenv("LORE_VERSION"),
		os.Getenv("LORE_BUILD_VERSION"),
	} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return defaultLoreBuildVersion
}

func resolveHookModuleVersion(flagValue string) string {
	for _, value := range []string{
		flagValue,
		os.Getenv("LOREHUB_HOOK_MODULE_VERSION"),
		os.Getenv("LOREHUB_HOOK_VERSION"),
		os.Getenv("LORE_HOOK_MODULE_VERSION"),
	} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return defaultHookModuleVersion
}
