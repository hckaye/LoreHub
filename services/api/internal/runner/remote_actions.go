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
	maxActionDepth         = 8
	maxActionRepositories  = 128
	maxActionTotalFiles    = 100_000
	maxActionTotalBytes    = int64(1 << 30)
)

var (
	actionNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	commitRefPattern  = regexp.MustCompile(`^(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)
)

type remoteAction struct {
	Uses    string
	Owner   string
	Repo    string
	Subpath string
	Ref     string
}

func (action remoteAction) repositoryKey() string {
	return action.Owner + "/" + action.Repo + "@" + action.Ref
}

func (action remoteAction) identity() string {
	subpath := action.Subpath
	if subpath == "" {
		subpath = "."
	}
	return action.repositoryKey() + "#" + subpath
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
	for _, part := range parts[2:] {
		if !actionNamePattern.MatchString(part) {
			return remoteAction{}, fmt.Errorf("remote Action reference %q has an invalid subpath", uses)
		}
	}
	if strings.Contains(ref, "..") || strings.ContainsAny(ref, "\\?#") || len(ref) > 128 {
		return remoteAction{}, fmt.Errorf("remote Action reference %q has an invalid ref", uses)
	}
	subpath, err := normalizeActionSubpath(strings.Join(parts[2:], "/"))
	if err != nil {
		return remoteAction{}, fmt.Errorf("remote Action reference %q has an invalid subpath: %w", uses, err)
	}
	return remoteAction{Uses: uses, Owner: parts[0], Repo: parts[1], Subpath: subpath, Ref: ref}, nil
}

func prepareRemoteActions(
	ctx context.Context,
	workflowPath string,
	actionDirectory string,
	sourceURL string,
	environment string,
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
	if err := validateActionSourceURL(sourceURL, environment); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(actionDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("create remote Action directory: %w", err)
	}
	resolver := &remoteActionResolver{
		client:         &http.Client{Timeout: 2 * time.Minute, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}},
		sourceURL:      strings.TrimRight(sourceURL, "/"),
		directory:      actionDirectory,
		mappings:       make(map[string]string),
		seenIdentities: make(map[string]struct{}),
		visiting:       make(map[string]bool),
	}
	for _, action := range actions {
		if err := resolver.resolve(ctx, action, 0); err != nil {
			return nil, fmt.Errorf("fetch remote Action %s: %w", action.Uses, err)
		}
	}
	keys := make([]string, 0, len(resolver.mappings))
	for key := range resolver.mappings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	mappings := make([]string, 0, len(keys))
	for _, key := range keys {
		mappings = append(mappings, key+"="+resolver.mappings[key])
	}
	return mappings, nil
}

func PrepareRemoteActions(
	ctx context.Context,
	workflowPath string,
	destination string,
	sourceURL string,
	environment string,
) ([]string, error) {
	return prepareRemoteActions(ctx, workflowPath, destination, sourceURL, environment)
}

type remoteActionResolver struct {
	client         *http.Client
	sourceURL      string
	directory      string
	mappings       map[string]string
	seenIdentities map[string]struct{}
	visiting       map[string]bool
	nextIndex      int
	totalFiles     int
	totalBytes     int64
}

func (resolver *remoteActionResolver) resolve(
	ctx context.Context,
	action remoteAction,
	depth int,
) error {
	if depth > maxActionDepth {
		return fmt.Errorf("remote Action recursion exceeds depth %d", maxActionDepth)
	}
	baseKey := action.repositoryKey()
	identity := action.identity()
	if resolver.visiting[identity] {
		return fmt.Errorf("remote Action dependency cycle detected at %s", identity)
	}
	destination, loaded := resolver.mappings[baseKey]
	if !loaded {
		if len(resolver.mappings) >= maxActionRepositories {
			return fmt.Errorf("remote Action dependency count exceeds %d", maxActionRepositories)
		}
		destination = filepath.Join(
			resolver.directory,
			fmt.Sprintf("%03d-%s-%s", resolver.nextIndex, action.Owner, action.Repo),
		)
		resolver.nextIndex++
		stats, err := fetchRemoteAction(ctx, resolver.client, resolver.sourceURL, action, destination)
		if err != nil {
			return err
		}
		resolver.mappings[baseKey] = destination
		resolver.totalFiles += stats.files
		resolver.totalBytes += stats.bytes
		if resolver.totalFiles > maxActionTotalFiles || resolver.totalBytes > maxActionTotalBytes {
			_ = os.RemoveAll(destination)
			delete(resolver.mappings, baseKey)
			return errors.New("remote Action dependency extraction exceeds the global quota")
		}
	}
	if _, seen := resolver.seenIdentities[identity]; seen {
		return nil
	}
	resolver.seenIdentities[identity] = struct{}{}
	resolver.visiting[identity] = true
	defer delete(resolver.visiting, identity)
	nested, err := compositeRemoteActions(destination, action.Subpath)
	if err != nil {
		return err
	}
	for _, nestedAction := range nested {
		if err := resolver.resolve(ctx, nestedAction, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func compositeRemoteActions(destination string, subpath string) ([]remoteAction, error) {
	manifest, err := readActionManifest(destination, subpath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var document yaml.Node
	if err := yaml.Unmarshal(manifest, &document); err != nil {
		return nil, fmt.Errorf("parse composite Action manifest: %w", err)
	}
	root, err := documentRoot(&document)
	if err != nil {
		return nil, err
	}
	runs := mappingValue(root, "runs")
	if runs == nil || runs.Kind != yaml.MappingNode {
		return nil, nil
	}
	using, err := scalarString(mappingValue(runs, "using"), "composite Action runs.using")
	if err != nil || using != "composite" {
		return nil, err
	}
	seen := make(map[string]remoteAction)
	if err := collectRemoteActions(runs, seen); err != nil {
		return nil, err
	}
	actions := make([]remoteAction, 0, len(seen))
	for _, action := range seen {
		actions = append(actions, action)
	}
	sort.Slice(actions, func(i int, j int) bool { return actions[i].Uses < actions[j].Uses })
	return actions, nil
}

func readActionManifest(destination string, subpath string) ([]byte, error) {
	cleanSubpath, err := normalizeActionSubpath(subpath)
	if err != nil {
		return nil, err
	}
	root := destination
	if cleanSubpath != "" {
		root = filepath.Join(root, filepath.FromSlash(cleanSubpath))
	}
	for _, name := range []string{"action.yml", "action.yaml"} {
		contents, err := os.ReadFile(filepath.Join(root, name))
		if err == nil {
			return contents, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	return nil, os.ErrNotExist
}

func normalizeActionSubpath(subpath string) (string, error) {
	cleanSubpath := path.Clean(strings.TrimPrefix(subpath, "/"))
	if cleanSubpath == "." {
		return "", nil
	}
	if cleanSubpath == ".." || strings.HasPrefix(cleanSubpath, "../") || path.IsAbs(cleanSubpath) {
		return "", errors.New("remote Action subpath escapes its repository")
	}
	return cleanSubpath, nil
}

type actionExtractionStats struct {
	files int
	bytes int64
}

func fetchRemoteAction(
	ctx context.Context,
	client *http.Client,
	sourceURL string,
	action remoteAction,
	destination string,
) (actionExtractionStats, error) {
	refs := []string{"refs/tags/", "refs/heads/"}
	if commitRefPattern.MatchString(action.Ref) {
		refs = []string{""}
	}
	var lastStatus int
	for _, prefix := range refs {
		archiveURL := sourceURL + "/" + action.Owner + "/" + action.Repo + "/archive/" +
			prefix + url.PathEscape(action.Ref) + ".tar.gz"
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL, nil)
		if err != nil {
			return actionExtractionStats{}, err
		}
		response, err := client.Do(request)
		if err != nil {
			return actionExtractionStats{}, err
		}
		if response.StatusCode == http.StatusNotFound {
			lastStatus = response.StatusCode
			_ = response.Body.Close()
			continue
		}
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			status := response.Status
			_ = response.Body.Close()
			return actionExtractionStats{}, fmt.Errorf("upstream returned %s", status)
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, maxActionArchiveBytes+1))
		_ = response.Body.Close()
		if err != nil {
			return actionExtractionStats{}, fmt.Errorf("read archive: %w", err)
		}
		if int64(len(body)) > maxActionArchiveBytes {
			return actionExtractionStats{}, errors.New("remote Action archive exceeds the download quota")
		}
		return extractActionArchiveStats(body, destination)
	}
	return actionExtractionStats{}, fmt.Errorf("upstream returned HTTP %d for the requested ref", lastStatus)
}

func extractActionArchive(archive []byte, destination string) error {
	_, err := extractActionArchiveStats(archive, destination)
	return err
}

func extractActionArchiveStats(archive []byte, destination string) (actionExtractionStats, error) {
	reader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return actionExtractionStats{}, fmt.Errorf("open archive: %w", err)
	}
	defer func() { _ = reader.Close() }()
	tarReader := tar.NewReader(reader)
	root := ""
	stats := actionExtractionStats{}
	cleanup := func() { _ = os.RemoveAll(destination) }
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			cleanup()
			return actionExtractionStats{}, fmt.Errorf("read archive entry: %w", err)
		}
		switch header.Typeflag {
		case tar.TypeXGlobalHeader, tar.TypeXHeader, tar.TypeGNULongName, tar.TypeGNULongLink:
			continue
		}
		archiveName := filepath.ToSlash(header.Name)
		if strings.HasPrefix(archiveName, "/") || strings.Contains(archiveName, "\x00") {
			cleanup()
			return actionExtractionStats{}, errors.New("remote Action archive has an invalid root path")
		}
		for _, part := range strings.Split(archiveName, "/") {
			if part == ".." {
				cleanup()
				return actionExtractionStats{}, errors.New("remote Action archive contains a path escape")
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
				return actionExtractionStats{}, errors.New("remote Action archive contains multiple roots")
			}
			if err := os.MkdirAll(destination, 0o700); err != nil {
				cleanup()
				return actionExtractionStats{}, err
			}
			continue
		}
		if len(parts) < 2 || parts[0] == "" {
			cleanup()
			return actionExtractionStats{}, errors.New("remote Action archive has an invalid root path")
		}
		if root == "" {
			root = parts[0]
		}
		if parts[0] != root {
			cleanup()
			return actionExtractionStats{}, errors.New("remote Action archive contains multiple roots")
		}
		relative := path.Join(parts[1:]...)
		if relative == "." || relative == ".." || strings.HasPrefix(relative, "../") || path.IsAbs(relative) {
			continue
		}
		target := filepath.Join(destination, filepath.FromSlash(relative))
		if header.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o700); err != nil {
				cleanup()
				return actionExtractionStats{}, err
			}
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			cleanup()
			return actionExtractionStats{}, fmt.Errorf("remote Action archive contains unsupported file %q", header.Name)
		}
		stats.files++
		if stats.files > maxActionFiles || header.Size < 0 || header.Size > maxActionUnpackedBytes-stats.bytes {
			cleanup()
			return actionExtractionStats{}, errors.New("remote Action archive exceeds the extraction quota")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			cleanup()
			return actionExtractionStats{}, err
		}
		file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
		if err != nil {
			cleanup()
			return actionExtractionStats{}, err
		}
		written, copyErr := io.CopyN(file, tarReader, header.Size)
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil || written != header.Size {
			cleanup()
			return actionExtractionStats{}, fmt.Errorf("write remote Action file %q", header.Name)
		}
		if header.Mode&0o111 != 0 {
			if err := os.Chmod(target, 0o700); err != nil {
				cleanup()
				return actionExtractionStats{}, err
			}
		}
		stats.bytes += written
	}
	if stats.files == 0 {
		cleanup()
		return actionExtractionStats{}, errors.New("remote Action archive contains no files")
	}
	return stats, nil
}
