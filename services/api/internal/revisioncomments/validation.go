package revisioncomments

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	defaultPage  = 1
	defaultLimit = 30
	maxLimit     = 100
	maxBodyBytes = 1_000_000
)

var errInvalidInput = errors.New("revision comment input is invalid")

func validateRevision(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) != 64 || strings.Trim(value, "0123456789abcdef") != "" {
		return "", errInvalidInput
	}
	return value, nil
}

func validateBody(value string) (string, error) {
	invalid := !utf8.ValidString(value) || len(value) > maxBodyBytes || strings.TrimSpace(value) == ""
	if invalid || hasInvalidControl(value) {
		return "", errInvalidInput
	}
	return value, nil
}

func hasInvalidControl(value string) bool {
	for _, character := range value {
		if character == '\u007f' || character < '\u0020' && character != '\n' && character != '\r' && character != '\t' {
			return true
		}
	}
	return false
}

func validateCommentID(value string) (string, error) {
	value = strings.TrimSpace(value)
	parsed, err := uuid.Parse(value)
	if err != nil || parsed.String() != value {
		return "", errInvalidInput
	}
	return value, nil
}

func parsePage(values url.Values) (int, int, error) {
	for key, entries := range values {
		if (key != "page" && key != "perPage") || len(entries) != 1 {
			return 0, 0, errInvalidInput
		}
	}
	pageValue := ""
	if entries, exists := values["page"]; exists {
		if entries[0] == "" {
			return 0, 0, errInvalidInput
		}
		pageValue = entries[0]
	}
	page, err := positive(pageValue, defaultPage, 1_000_000)
	if err != nil {
		return 0, 0, err
	}
	perPageValue := ""
	if entries, exists := values["perPage"]; exists {
		if entries[0] == "" {
			return 0, 0, errInvalidInput
		}
		perPageValue = entries[0]
	}
	perPage, err := positive(perPageValue, defaultLimit, maxLimit)
	if err != nil {
		return 0, 0, err
	}
	return page, perPage, nil
}

func positive(value string, fallback int, maximum int) (int, error) {
	if value == "" {
		return fallback, nil
	}
	number, err := strconv.ParseUint(value, 10, 32)
	if err != nil || number < 1 || number > uint64(maximum) {
		return 0, errInvalidInput
	}
	return int(number), nil
}
