package code

import (
	"encoding/hex"
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	maxPathLength       = 2_048
	maxRevisionLength   = 64
	maxTreeLimit        = 2_000
	maxHistoryLimit     = 500
	maxDiffFiles        = 500
	maxDiffPatchBytes   = 8 << 20
	maxFileBytes        = 4 << 20
	maxDiffPathCount    = 100
	maxRevisionQueryLen = 128
)

var (
	errInvalidPath     = errors.New("invalid repository path")
	errInvalidRevision = errors.New("invalid Lore revision")
)

func normalizePath(value string) (string, error) {
	if len(value) > maxPathLength || value == "" {
		if value == "" {
			return "", nil
		}
		return "", errInvalidPath
	}
	if !utf8.ValidString(value) || strings.IndexByte(value, 0) >= 0 || strings.ContainsRune(value, '\\') {
		return "", errInvalidPath
	}
	if strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || strings.Contains(value, "//") {
		return "", errInvalidPath
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return "", errInvalidPath
		}
	}
	return value, nil
}

func normalizeRevision(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) != maxRevisionLength || len(value) > maxRevisionQueryLen {
		return "", errInvalidRevision
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", errInvalidRevision
	}
	return strings.ToLower(value), nil
}

func normalizeBranch(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 255 || !utf8.ValidString(value) || strings.IndexByte(value, 0) >= 0 {
		return "", errors.New("invalid branch")
	}
	if strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || strings.Contains(value, "//") ||
		strings.Contains(value, "..") || strings.ContainsRune(value, '\\') {
		return "", errors.New("invalid branch")
	}
	return value, nil
}

func boundedInt(value string, fallback int, maximum int) int {
	if maximum < 1 {
		return fallback
	}
	var result int
	for _, character := range value {
		if character < '0' || character > '9' {
			return fallback
		}
		digit := int(character - '0')
		if result > (maximum-digit)/10 {
			return maximum
		}
		result = result*10 + digit
	}
	if result < 1 {
		return fallback
	}
	return result
}
