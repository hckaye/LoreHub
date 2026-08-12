package statuses

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxContextRunes       = 100
	maxDescriptionRunes   = 140
	maxTargetURLBytes     = 8_192
	maxIdempotencyRunes   = 255
	defaultStatusContext  = "default"
	defaultHistoryPerPage = 30
	maxHistoryPerPage     = 100
)

func validateRevision(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) != 64 || strings.Trim(value, "0123456789abcdef") != "" {
		return "", invalid("revision must be one lowercase Lore revision")
	}
	return value, nil
}

func validateCreate(input CreateInput) (CreateInput, error) {
	revision, err := validateRevision(input.Revision)
	if err != nil {
		return CreateInput{}, err
	}
	input.Revision = revision
	input.Context = strings.TrimSpace(input.Context)
	input.State = strings.TrimSpace(input.State)
	input.Description = strings.TrimSpace(input.Description)
	input.TargetURL = strings.TrimSpace(input.TargetURL)
	if input.Context == "" {
		input.Context = defaultStatusContext
	}
	if !validSingleLine(input.Context, 1, maxContextRunes) {
		return CreateInput{}, invalid("context is required and must not exceed 100 characters")
	}
	if input.State != "pending" && input.State != "success" &&
		input.State != "failure" && input.State != "error" {
		return CreateInput{}, invalid("state must be pending, success, failure, or error")
	}
	if !validSingleLine(input.Description, 0, maxDescriptionRunes) {
		return CreateInput{}, invalid("description must not exceed 140 characters")
	}
	if err := validateTargetURL(input.TargetURL); err != nil {
		return CreateInput{}, err
	}
	if input.IdempotencyKey != nil {
		value := strings.TrimSpace(*input.IdempotencyKey)
		if !validSingleLine(value, 1, maxIdempotencyRunes) {
			return CreateInput{}, invalid("idempotencyKey must not exceed 255 characters")
		}
		input.IdempotencyKey = &value
	}
	return input, nil
}

func validateTargetURL(value string) error {
	if value == "" {
		return nil
	}
	if len(value) > maxTargetURLBytes || !utf8.ValidString(value) || hasControl(value) {
		return invalid("targetUrl is invalid")
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Host == "" || parsed.User != nil ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return invalid("targetUrl must be an HTTP or HTTPS URL without credentials")
	}
	return nil
}

func validSingleLine(value string, minimum, maximum int) bool {
	if !utf8.ValidString(value) {
		return false
	}
	length := utf8.RuneCountInString(value)
	if length < minimum || length > maximum {
		return false
	}
	return !hasControl(value)
}

func hasControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}

func invalid(detail string) error {
	return fmt.Errorf("%w: %s", ErrInvalidInput, detail)
}
