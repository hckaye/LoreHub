package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const ReadLoreScope = "read"

type CredentialSubject struct {
	RepositoryID string
	LoreURL      string
}

type CredentialProvider interface {
	Read(ctx context.Context, subject CredentialSubject, scope string) (string, error)
}

type developmentCredentialProvider struct {
	identity string
}

func NewDevelopmentCredentialProvider(identity string) CredentialProvider {
	return developmentCredentialProvider{identity: identity}
}

func (provider developmentCredentialProvider) Read(
	ctx context.Context,
	subject CredentialSubject,
	scope string,
) (string, error) {
	if err := validateCredentialRequest(ctx, subject, scope); err != nil {
		return "", err
	}
	if strings.TrimSpace(provider.identity) == "" {
		return "", errors.New("development Lore identity is empty")
	}
	return provider.identity, nil
}

type fileCredentialProvider struct {
	root string
}

func NewFileCredentialProvider(root string) (CredentialProvider, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("Lore credential directory is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve Lore credential directory: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return nil, fmt.Errorf("inspect Lore credential directory: %w", err)
	}
	if !info.IsDir() {
		return nil, errors.New("Lore credential directory is not a directory")
	}
	return fileCredentialProvider{root: absolute}, nil
}

func (provider fileCredentialProvider) Read(
	ctx context.Context,
	subject CredentialSubject,
	scope string,
) (string, error) {
	if err := validateCredentialRequest(ctx, subject, scope); err != nil {
		return "", err
	}
	if strings.ContainsAny(subject.RepositoryID, `/\\`) || subject.RepositoryID == "." || subject.RepositoryID == ".." {
		return "", errors.New("repository credential partition is invalid")
	}
	path := filepath.Join(provider.root, subject.RepositoryID, scope)
	if err := rejectCredentialSymlinks(provider.root, path); err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("read repository Lore credential: %w", err)
	}
	defer func() { _ = file.Close() }()
	contents, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return "", fmt.Errorf("read repository Lore credential: %w", err)
	}
	if len(contents) > 4096 {
		return "", errors.New("repository Lore credential exceeds 4 KiB")
	}
	identity := strings.TrimSpace(string(contents))
	if identity == "" {
		return "", errors.New("repository Lore credential is empty")
	}
	return identity, nil
}

func validateCredentialRequest(ctx context.Context, subject CredentialSubject, scope string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if subject.RepositoryID == "" || subject.LoreURL == "" {
		return errors.New("repository partition and Lore URL are required for a credential request")
	}
	if scope != ReadLoreScope {
		return fmt.Errorf("Lore credential scope %q is not supported", scope)
	}
	return nil
}

func rejectCredentialSymlinks(root string, target string) error {
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return fmt.Errorf("inspect Lore credential directory: %w", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return errors.New("Lore credential directory is not a real directory")
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("Lore credential path escapes its partition")
	}
	current := root
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if statErr != nil {
			return fmt.Errorf("inspect Lore credential path: %w", statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("Lore credential path contains a symlink")
		}
	}
	return nil
}
