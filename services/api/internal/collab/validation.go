package collab

import (
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	maxBodyBytes      = 1_000_000
	maxTitleLen       = 512
	maxLabelNameLen   = 128
	maxDescriptionLen = 10_000
	maxPatternLen     = 255
	maxReviewBodyLen  = 1_000_000
	defaultPageLimit  = 30
	maxPageLimit      = 100
)

var (
	ErrBlankBody        = errors.New("body must not be blank")
	ErrTitleTooLong     = errors.New("title is too long")
	ErrBodyTooLong      = errors.New("body is too long")
	ErrInvalidState     = errors.New("state is invalid")
	ErrInvalidColor     = errors.New("color must be a six-digit hex value")
	ErrInvalidLabel     = errors.New("label name is invalid")
	ErrInvalidDecision  = errors.New("review decision is invalid")
	ErrInvalidPattern   = errors.New("branch pattern is invalid")
	ErrInvalidApprovals = errors.New("required approvals must be between 0 and 100")
)

var (
	labelNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _-]{0,127}$`)
	hexColorPattern  = regexp.MustCompile(`^[0-9A-Fa-f]{6}$`)
)

// validateIssueTitle trims and validates an issue/merge-request title.
func validateTitle(value string) (string, error) {
	title := strings.TrimSpace(value)
	if title == "" {
		return "", ErrBlankBody
	}
	if len(title) > maxTitleLen {
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

// normalizeLabelName trims surrounding whitespace and collapses internal runs
// of whitespace into single spaces, matching the stored normalized form.
func normalizeLabelName(value string) string {
	value = strings.TrimSpace(value)
	for strings.Contains(value, "  ") {
		value = strings.ReplaceAll(value, "  ", " ")
	}
	return value
}

func validateLabelInput(input LabelInput) (LabelInput, error) {
	name := normalizeLabelName(input.Name)
	if !labelNamePattern.MatchString(name) {
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
	return BranchRuleInput{
		Pattern:           pattern,
		RequiredApprovals: input.RequiredApprovals,
		RequireCISuccess:  input.RequireCISuccess,
		BlockDirectPush:   input.BlockDirectPush,
	}, nil
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
