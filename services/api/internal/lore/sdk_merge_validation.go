package lore

import (
	"errors"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func sameMergeParents(parents []string, sourceRevision, targetRevision string) bool {
	if len(parents) != 2 {
		return false
	}
	return (parents[0] == sourceRevision && parents[1] == targetRevision) ||
		(parents[0] == targetRevision && parents[1] == sourceRevision)
}

func mergeWorkspacePaths(workspace string, paths []string) []string {
	resolved := make([]string, 0, len(paths))
	for _, path := range paths {
		if filepath.IsAbs(path) {
			resolved = append(resolved, path)
			continue
		}
		resolved = append(resolved, filepath.Join(workspace, path))
	}
	return resolved
}

func validateMergeWorkspacePaths(paths []string) error {
	if len(paths) > maxConflictPaths-1 {
		return errors.New("too many Lore merge paths")
	}
	for _, path := range paths {
		if path == "" || len(path) > 2_048 || !utf8.ValidString(path) || strings.IndexByte(path, 0) >= 0 ||
			strings.ContainsRune(path, '\\') || filepath.IsAbs(path) {
			return errors.New("invalid Lore merge path")
		}
		for _, part := range strings.Split(path, "/") {
			if part == "" || part == "." || part == ".." {
				return errors.New("invalid Lore merge path")
			}
		}
	}
	return nil
}

func appendUnique(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func safeOperationPart(value string) string {
	value = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, value)
	return strings.Trim(value, "-")
}

func branchLatestRevision(branches []Branch, name string) (string, bool) {
	for _, branch := range branches {
		if branch.Name == name && !branch.Archived && branch.LatestRevision != "" {
			return branch.LatestRevision, true
		}
	}
	return "", false
}
