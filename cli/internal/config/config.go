package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const DefaultHost = "lorehub.com"

const hostsFileMode os.FileMode = 0o600

type HostConfig struct {
	Token       string `yaml:"token,omitempty"`
	DefaultRepo string `yaml:"defaultRepo,omitempty"`
}

type Hosts map[string]HostConfig

type Store struct {
	path string
}

func NewStore(path string) *Store {
	if strings.TrimSpace(path) == "" {
		path = DefaultPath()
	}
	return &Store{path: path}
}

func DefaultStore() *Store {
	return NewStore(DefaultPath())
}

func DefaultPath() string {
	configHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err == nil && home != "" {
			configHome = filepath.Join(home, ".config")
		} else {
			configHome = ".config"
		}
	}
	return filepath.Join(configHome, "lh", "hosts.yml")
}

func (s *Store) Path() string {
	return s.path
}

func (s *Store) Load() (Hosts, error) {
	contents, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return Hosts{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read hosts file: %w", err)
	}
	if len(strings.TrimSpace(string(contents))) == 0 {
		return Hosts{}, nil
	}

	var hosts Hosts
	if err := yaml.Unmarshal(contents, &hosts); err != nil {
		return nil, fmt.Errorf("parse hosts file: %w", err)
	}
	if hosts == nil {
		hosts = Hosts{}
	}
	return hosts, nil
}

func (s *Store) Save(hosts Hosts) error {
	if hosts == nil {
		hosts = Hosts{}
	}
	contents, err := yaml.Marshal(hosts)
	if err != nil {
		return fmt.Errorf("encode hosts file: %w", err)
	}

	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".hosts.yml-*")
	if err != nil {
		return fmt.Errorf("create temporary hosts file: %w", err)
	}
	temporaryName := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryName)
		}
	}()

	if err := temporary.Chmod(hostsFileMode); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set hosts file permissions: %w", err)
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write hosts file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync hosts file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close hosts file: %w", err)
	}
	if err := os.Rename(temporaryName, s.path); err != nil {
		return fmt.Errorf("replace hosts file: %w", err)
	}
	removeTemporary = false
	return nil
}

func NormalizeHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	return strings.TrimRight(host, "/")
}

func ResolveHost(explicit string, defaultHost string) string {
	if explicit = NormalizeHost(explicit); explicit != "" {
		return explicit
	}
	if configured := NormalizeHost(os.Getenv("LH_HOST")); configured != "" {
		return configured
	}
	if defaultHost = NormalizeHost(defaultHost); defaultHost != "" {
		return defaultHost
	}
	return DefaultHost
}

func ResolveToken(fileToken string) (string, string) {
	if token, ok := os.LookupEnv("LH_TOKEN"); ok && strings.TrimSpace(token) != "" {
		return strings.TrimSpace(token), "environment"
	}
	return strings.TrimSpace(fileToken), "hosts file"
}

func ParseRepo(value string) (string, error) {
	value = strings.TrimSpace(value)
	parts := strings.Split(value, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" ||
		strings.ContainsAny(parts[0], " \t\r\n") || strings.ContainsAny(parts[1], " \t\r\n") {
		return "", fmt.Errorf("repository must be OWNER/NAME")
	}
	return parts[0] + "/" + parts[1], nil
}
