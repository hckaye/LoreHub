package collab

import (
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	maxBodyBytes        = 1_000_000
	maxTitleLen         = 512
	maxLabelNameLen     = 128
	maxDescriptionLen   = 10_000
	maxPatternLen       = 255
	maxStatusContexts   = 50
	maxStatusContextLen = 100
	maxReviewBodyLen    = 1_000_000
	defaultPageLimit    = 30
	maxPageLimit        = 100
)

var (
	ErrBlankBody           = errors.New("body must not be blank")
	ErrTitleTooLong        = errors.New("title is too long")
	ErrBodyTooLong         = errors.New("body is too long")
	ErrInvalidState        = errors.New("state is invalid")
	ErrInvalidColor        = errors.New("color must be a six-digit hex value")
	ErrInvalidLabel        = errors.New("label name is invalid")
	ErrInvalidDecision     = errors.New("review decision is invalid")
	ErrInvalidPattern      = errors.New("branch pattern is invalid")
	ErrInvalidApprovals    = errors.New("required approvals must be between 0 and 100")
	ErrInvalidStatusChecks = errors.New("required status checks are invalid")
	ErrInvalidPrecondition = errors.New("If-Match must be a valid timestamp")
)

var (
	hexColorPattern = regexp.MustCompile(`^[0-9A-Fa-f]{6}$`)
)

// validateIssueTitle trims and validates an issue/merge-request title.
func validateTitle(value string) (string, error) {
	title := strings.TrimSpace(value)
	if title == "" {
		return "", ErrBlankBody
	}
	if utf8.RuneCountInString(title) > maxTitleLen {
		return "", ErrTitleTooLong
	}
	return title, nil
}

// validateBody validates a free-form body. Blank bodies are allowed for edits
// that omit the body field; pass requireNonBlank=true for comment bodies.
func validateBody(value string, requireNonBlank bool) (string, error) {
	if len(value) > maxBodyBytes {
		return "", ErrBodyTooLong
	}
	trimmed := strings.TrimSpace(value)
	if requireNonBlank && trimmed == "" {
		return "", ErrBlankBody
	}
	return value, nil
}

// validateIssueState validates an issue/merge-request state transition target.
func validateIssueState(value string) (string, error) {
	switch value {
	case "open", "closed":
		return value, nil
	default:
		return "", ErrInvalidState
	}
}

// validateMergeRequestState restricts merge-request state to open/closed; the
// merged state is terminal and not reachable through the PATCH endpoint.
func validateMergeRequestState(value string) (string, error) {
	switch value {
	case "open", "closed":
		return value, nil
	default:
		return "", ErrInvalidState
	}
}

// normalizeLabelName trims surrounding Unicode whitespace and collapses
// internal runs of Unicode whitespace into single spaces.
func normalizeLabelName(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	spacePending := false
	for _, r := range value {
		if unicode.IsSpace(r) {
			if builder.Len() > 0 {
				spacePending = true
			}
			continue
		}
		if spacePending {
			builder.WriteByte(' ')
		}
		builder.WriteRune(r)
		spacePending = false
	}
	return builder.String()
}

func hasForbiddenLabelRune(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return true
		}
	}
	return false
}

func validateLabelInput(input LabelInput) (LabelInput, error) {
	if hasForbiddenLabelRune(input.Name) {
		return LabelInput{}, ErrInvalidLabel
	}
	name := normalizeLabelName(input.Name)
	if name == "" || utf8.RuneCountInString(name) > maxLabelNameLen {
		return LabelInput{}, ErrInvalidLabel
	}
	description := strings.TrimSpace(input.Description)
	if len(description) > maxDescriptionLen {
		return LabelInput{}, ErrBodyTooLong
	}
	color := strings.TrimSpace(input.Color)
	if !hexColorPattern.MatchString(color) {
		return LabelInput{}, ErrInvalidColor
	}
	return LabelInput{Name: name, Description: description, Color: color}, nil
}

func validateReviewInput(input ReviewInput) (ReviewInput, error) {
	decision := strings.TrimSpace(input.Decision)
	switch decision {
	case "approved", "changes_requested", "commented":
	default:
		return ReviewInput{}, ErrInvalidDecision
	}
	body, err := validateBody(input.Body, false)
	if err != nil {
		return ReviewInput{}, err
	}
	if len(body) > maxReviewBodyLen {
		return ReviewInput{}, ErrBodyTooLong
	}
	return ReviewInput{Decision: decision, Body: body}, nil
}

func validateBranchRuleInput(input BranchRuleInput) (BranchRuleInput, error) {
	pattern := strings.TrimSpace(input.Pattern)
	if pattern == "" || len(pattern) > maxPatternLen {
		return BranchRuleInput{}, ErrInvalidPattern
	}
	if strings.ContainsAny(pattern, "\x00\n\r") {
		return BranchRuleInput{}, ErrInvalidPattern
	}
	if input.RequiredApprovals < 0 || input.RequiredApprovals > 100 {
		return BranchRuleInput{}, ErrInvalidApprovals
	}
	statusChecks, err := validateRequiredStatusChecks(input.RequiredStatusChecks)
	if err != nil {
		return BranchRuleInput{}, err
	}
	return BranchRuleInput{
		Pattern:              pattern,
		RequiredApprovals:    input.RequiredApprovals,
		RequireCISuccess:     input.RequireCISuccess,
		RequiredStatusChecks: statusChecks,
		BlockDirectPush:      input.BlockDirectPush,
	}, nil
}

func validateRequiredStatusChecks(values []string) ([]string, error) {
	if len(values) > maxStatusContexts {
		return nil, ErrInvalidStatusChecks
	}
	checks := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		contextName := strings.TrimSpace(value)
		if contextName == "" || utf8.RuneCountInString(contextName) > maxStatusContextLen ||
			hasControlRune(contextName) {
			return nil, ErrInvalidStatusChecks
		}
		key := strings.ToLower(contextName)
		if _, duplicate := seen[key]; duplicate {
			return nil, ErrInvalidStatusChecks
		}
		seen[key] = struct{}{}
		checks = append(checks, contextName)
	}
	sort.SliceStable(checks, func(left, right int) bool {
		return strings.ToLower(checks[left]) < strings.ToLower(checks[right])
	})
	return checks, nil
}

func hasControlRune(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// parsePage extracts a bounded pagination window from query parameters. The
// cursor is a simple base-10 offset string; absent cursors start at offset 0.
func parsePage(values url.Values) (Page, int, error) {
	limit := defaultPageLimit
	if raw := values.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			return Page{}, 0, errors.New("limit must be a positive integer")
		}
		limit = parsed
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}
	offset := 0
	cursor := ""
	if raw := values.Get("cursor"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			return Page{}, 0, errors.New("cursor must be a non-negative integer")
		}
		offset = parsed
		cursor = raw
	}
	return Page{Limit: limit, Cursor: cursor}, offset, nil
}

// encodeCursor returns the next-page cursor for an offset-based window.
func encodeCursor(offset int, limit int, returned int) string {
	if returned < limit {
		return ""
	}
	next := offset + returned
	return strconv.Itoa(next)
}

// parseCursor parses an opaque offset cursor string.
func parseCursor(cursor string) (int, error) {
	parsed, err := strconv.Atoi(cursor)
	if err != nil || parsed < 0 {
		return 0, errors.New("cursor must be a non-negative integer")
	}
	return parsed, nil
}

// parseIfMatch parses an If-Match header carrying an RFC3339 updated_at value
// used for optimistic concurrency. An empty header returns a zero time and
// ok=false, indicating the client did not request a precondition.
func parseIfMatch(header string) (time.Time, bool) {
	header = strings.TrimSpace(header)
	if header == "" {
		return time.Time{}, false
	}
	value := strings.Trim(header, `"`)
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}
