package runner

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	defaultActionSourceURL = "https://github.com"
	maxActionArchiveBytes  = int64(64 << 20)
	maxActionFiles         = 20_000
	maxActionUnpackedBytes = int64(256 << 20)
)

var actionNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

type remoteAction struct {
	Uses  string
	Owner string
	Repo  string
	Ref   string
}

func validateActionSourceURL(value string, environment string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("action source URL is required")
	}
	parsed, err := validatePublicURL(value, environment, true)
	if err != nil {
		return fmt.Errorf("action source URL is invalid: %w", err)
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return errors.New("action source URL must contain only an origin")
	}
	return nil
}

func workflowRemoteActions(workflowPath string) ([]remoteAction, error) {
	contents, err := os.ReadFile(workflowPath)
	if err != nil {
		return nil, fmt.Errorf("read workflow for remote Actions: %w", err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(contents, &document); err != nil {
		return nil, fmt.Errorf("parse workflow for remote Actions: %w", err)
	}
	seen := make(map[string]remoteAction)
	if err := collectRemoteActions(&document, seen); err != nil {
		return nil, err
	}
	actions := make([]remoteAction, 0, len(seen))
	for _, action := range seen {
		actions = append(actions, action)
	}
	sort.Slice(actions, func(i int, j int) bool { return actions[i].Uses < actions[j].Uses })
	return actions, nil
}

func collectRemoteActions(node *yaml.Node, seen map[string]remoteAction) error {
	if node.Kind == yaml.MappingNode {
		for index := 0; index+1 < len(node.Content); index += 2 {
			if node.Content[index].Value != "uses" {
				continue
			}
			value := node.Content[index+1]
			if value.Kind != yaml.ScalarNode {
				return errors.New("Actions uses value must be a scalar")
			}
			uses := strings.TrimSpace(value.Value)
			if uses == "" || strings.HasPrefix(uses, "./") || strings.HasPrefix(uses, "docker://") {
				continue
			}
			if strings.HasPrefix(uses, "actions/checkout@") {
				continue
			}
			action, err := parseRemoteAction(uses)
			if err != nil {
				return err
			}
			seen[uses] = action
		}
	}
	for _, child := range node.Content {
		if err := collectRemoteActions(child, seen); err != nil {
			return err
		}
	}
	return nil
}

func parseRemoteAction(uses string) (remoteAction, error) {
	if strings.ContainsAny(uses, "\r\n\t ${}") {
		return remoteAction{}, fmt.Errorf("remote Action reference %q contains unsupported characters", uses)
	}
	pathRef, ref, found := strings.Cut(uses, "@")
	if !found || pathRef == "" || ref == "" || strings.Contains(ref, "@") {
		return remoteAction{}, fmt.Errorf("remote Action reference %q must use owner/repository@ref", uses)
	}
	parts := strings.Split(pathRef, "/")
	if len(parts) < 2 || !actionNamePattern.MatchString(parts[0]) || !actionNamePattern.MatchString(parts[1]) {
		return remoteAction{}, fmt.Errorf("remote Action reference %q has an invalid owner or repository", uses)
	}
	if strings.Contains(ref, "..") || strings.ContainsAny(ref, "\\?#") || len(ref) > 128 {
		return remoteAction{}, fmt.Errorf("remote Action reference %q has an invalid ref", uses)
	}
	return remoteAction{Uses: uses, Owner: parts[0], Repo: parts[1], Ref: ref}, nil
}

func prepareRemoteActions(
	ctx context.Context,
	workflowPath string,
	actionDirectory string,
	sourceURL string,
) ([]string, error) {
	actions, err := workflowRemoteActions(workflowPath)
	if err != nil {
		return nil, err
	}
	if len(actions) == 0 {
		return nil, nil
	}
	if sourceURL == "" {
		sourceURL = defaultActionSourceURL
	}
	if err := validateActionSourceURL(sourceURL, "development"); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(actionDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("create remote Action directory: %w", err)
	}
	client := &http.Client{
		Timeout:   2 * time.Minute,
		Transport: &http.Transport{Proxy: http.ProxyFromEnvironment},
	}
	mappings := make([]string, 0, len(actions))
	for index, action := range actions {
		destination := filepath.Join(actionDirectory, fmt.Sprintf("%03d-%s-%s", index, action.Owner, action.Repo))
		if err := fetchRemoteAction(ctx, client, strings.TrimRight(sourceURL, "/"), action, destination); err != nil {
			return nil, fmt.Errorf("fetch remote Action %s: %w", action.Uses, err)
		}
		mappings = append(mappings, action.Uses+"="+destination)
	}
	return mappings, nil
}

func fetchRemoteAction(
	ctx context.Context,
	client *http.Client,
	sourceURL string,
	action remoteAction,
	destination string,
) error {
	refs := []string{"refs/tags/", "refs/heads/"}
	var lastStatus int
	for _, prefix := range refs {
		archiveURL := sourceURL + "/" + action.Owner + "/" + action.Repo + "/archive/" +
			prefix + url.PathEscape(action.Ref) + ".tar.gz"
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL, nil)
		if err != nil {
			return err
		}
		response, err := client.Do(request)
		if err != nil {
			return err
		}
		if response.StatusCode == http.StatusNotFound {
			lastStatus = response.StatusCode
			_ = response.Body.Close()
			continue
		}
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			status := response.Status
			_ = response.Body.Close()
			return fmt.Errorf("upstream returned %s", status)
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, maxActionArchiveBytes+1))
		_ = response.Body.Close()
		if err != nil {
			return fmt.Errorf("read archive: %w", err)
		}
		if int64(len(body)) > maxActionArchiveBytes {
			return errors.New("remote Action archive exceeds the download quota")
		}
		return extractActionArchive(body, destination)
	}
	return fmt.Errorf("upstream returned HTTP %d for tag and branch refs", lastStatus)
}

func extractActionArchive(archive []byte, destination string) error {
	reader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer func() { _ = reader.Close() }()
	tarReader := tar.NewReader(reader)
	root := ""
	files := 0
	var total int64
	cleanup := func() { _ = os.RemoveAll(destination) }
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			cleanup()
			return fmt.Errorf("read archive entry: %w", err)
		}
		switch header.Typeflag {
		case tar.TypeXGlobalHeader, tar.TypeXHeader, tar.TypeGNULongName, tar.TypeGNULongLink:
			continue
		}
		archiveName := filepath.ToSlash(header.Name)
		if strings.HasPrefix(archiveName, "/") || strings.Contains(archiveName, "\x00") {
			cleanup()
			return errors.New("remote Action archive has an invalid root path")
		}
		for _, part := range strings.Split(archiveName, "/") {
			if part == ".." {
				cleanup()
				return errors.New("remote Action archive contains a path escape")
			}
		}
		archiveName = path.Clean(archiveName)
		parts := strings.Split(archiveName, "/")
		if len(parts) == 1 && parts[0] != "" && header.FileInfo().IsDir() {
			if root == "" {
				root = parts[0]
			}
			if parts[0] != root {
				cleanup()
				return errors.New("remote Action archive contains multiple roots")
			}
			if err := os.MkdirAll(destination, 0o700); err != nil {
				cleanup()
				return err
			}
			continue
		}
		if len(parts) < 2 || parts[0] == "" {
			cleanup()
			return errors.New("remote Action archive has an invalid root path")
		}
		if root == "" {
			root = parts[0]
		}
		if parts[0] != root {
			cleanup()
			return errors.New("remote Action archive contains multiple roots")
		}
		relative := path.Join(parts[1:]...)
		if relative == "." || relative == ".." || strings.HasPrefix(relative, "../") || path.IsAbs(relative) {
			continue
		}
		target := filepath.Join(destination, filepath.FromSlash(relative))
		if header.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o700); err != nil {
				cleanup()
				return err
			}
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			cleanup()
			return fmt.Errorf("remote Action archive contains unsupported file %q", header.Name)
		}
		files++
		if files > maxActionFiles || header.Size < 0 || header.Size > maxActionUnpackedBytes-total {
			cleanup()
			return errors.New("remote Action archive exceeds the extraction quota")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			cleanup()
			return err
		}
		file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
		if err != nil {
			cleanup()
			return err
		}
		written, copyErr := io.CopyN(file, tarReader, header.Size)
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil || written != header.Size {
			cleanup()
			return fmt.Errorf("write remote Action file %q", header.Name)
		}
		if header.Mode&0o111 != 0 {
			if err := os.Chmod(target, 0o700); err != nil {
				cleanup()
				return err
			}
		}
		total += written
	}
	if files == 0 {
		cleanup()
		return errors.New("remote Action archive contains no files")
	}
	return nil
}
