package config

import (
	"testing"
	"time"
)

func TestRepositoryDeletionConfigurationFailsClosed(t *testing.T) {
	valid := Config{
		LoreRepositoryLifecycleSubject:  "00000000-0000-4000-8000-000000000005",
		RepositoryDeletionRetention:     30 * 24 * time.Hour,
		RepositoryDeletionPollPeriod:    30 * time.Second,
		RepositoryDeletionTimeout:       90 * time.Second,
		RepositoryDeletionLeaseDuration: 2 * time.Minute,
	}
	if err := validateRepositoryDeletionConfig(valid, "serve"); err != nil {
		t.Fatalf("valid repository deletion configuration: %v", err)
	}
	cases := map[string]func(*Config){
		"subject": func(settings *Config) {
			settings.LoreRepositoryLifecycleSubject = ""
		},
		"retention": func(settings *Config) {
			settings.RepositoryDeletionRetention = 30 * time.Minute
		},
		"poll": func(settings *Config) {
			settings.RepositoryDeletionPollPeriod = 2 * time.Minute
		},
		"timeout": func(settings *Config) {
			settings.RepositoryDeletionTimeout = 11 * time.Minute
		},
		"lease": func(settings *Config) {
			settings.RepositoryDeletionLeaseDuration = time.Minute
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			settings := valid
			mutate(&settings)
			if err := validateRepositoryDeletionConfig(settings, "serve"); err == nil {
				t.Fatal("invalid repository deletion configuration was accepted")
			}
		})
	}
	if err := validateRepositoryDeletionConfig(Config{}, "runner"); err != nil {
		t.Fatalf("runner required unused repository deletion settings: %v", err)
	}
}
