package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type Config struct {
	Environment       string
	HTTPAddress       string
	DatabaseURL       string
	DatabaseTimeout   time.Duration
	ShutdownTimeout   time.Duration
	OIDCIssuer        string
	OIDCAudience      string
	LoreCacheDir      string
	LoreIdentity      string
	LoreBinary        string
	ActBinary         string
	RunnerPollPeriod  time.Duration
	BranchPollPeriod  time.Duration
	RunnerJobTimeout  time.Duration
	RunnerLogDir      string
	RunnerArtifactDir string
	RunnerWorkDir     string
}

func Load() (Config, error) {
	config := Config{
		Environment:       envOrDefault("LOREHUB_ENV", "development"),
		HTTPAddress:       envOrDefault("LOREHUB_HTTP_ADDRESS", ":8080"),
		DatabaseURL:       os.Getenv("DATABASE_URL"),
		OIDCIssuer:        os.Getenv("LOREHUB_OIDC_ISSUER"),
		OIDCAudience:      os.Getenv("LOREHUB_OIDC_AUDIENCE"),
		LoreCacheDir:      envOrDefault("LOREHUB_LORE_CACHE_DIR", ".cache/lorehub/repositories"),
		LoreIdentity:      os.Getenv("LOREHUB_LORE_IDENTITY"),
		LoreBinary:        envOrDefault("LOREHUB_LORE_BINARY", "lore"),
		ActBinary:         envOrDefault("LOREHUB_ACT_BINARY", "act"),
		DatabaseTimeout:   durationOrDefault("LOREHUB_DATABASE_TIMEOUT", 10*time.Second),
		ShutdownTimeout:   durationOrDefault("LOREHUB_SHUTDOWN_TIMEOUT", 15*time.Second),
		RunnerPollPeriod:  durationOrDefault("LOREHUB_RUNNER_POLL_PERIOD", 2*time.Second),
		BranchPollPeriod:  durationOrDefault("LOREHUB_BRANCH_POLL_PERIOD", 15*time.Second),
		RunnerJobTimeout:  durationOrDefault("LOREHUB_RUNNER_JOB_TIMEOUT", 60*time.Minute),
		RunnerLogDir:      envOrDefault("LOREHUB_RUNNER_LOG_DIR", ".cache/lorehub/runner-logs"),
		RunnerArtifactDir: envOrDefault("LOREHUB_RUNNER_ARTIFACT_DIR", ".cache/lorehub/runner-artifacts"),
		RunnerWorkDir:     envOrDefault("LOREHUB_RUNNER_WORK_DIR", ".cache/lorehub/runner-work"),
	}

	if config.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if config.Environment == "production" && (config.OIDCIssuer == "" || config.OIDCAudience == "") {
		return Config{}, errors.New("OIDC issuer and audience are required in production")
	}
	absCacheDir, err := filepath.Abs(config.LoreCacheDir)
	if err != nil {
		return Config{}, fmt.Errorf("resolve Lore cache directory: %w", err)
	}
	config.LoreCacheDir = absCacheDir
	for source, target := range map[string]*string{
		config.RunnerArtifactDir: &config.RunnerArtifactDir,
		config.RunnerLogDir:      &config.RunnerLogDir,
		config.RunnerWorkDir:     &config.RunnerWorkDir,
	} {
		absolute, err := filepath.Abs(source)
		if err != nil {
			return Config{}, fmt.Errorf("resolve runner directory: %w", err)
		}
		*target = absolute
	}

	return config, nil
}

func envOrDefault(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func durationOrDefault(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err == nil {
		return duration
	}
	seconds, err := strconv.Atoi(value)
	if err == nil {
		return time.Duration(seconds) * time.Second
	}
	return fallback
}
