package webhooks

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	ErrInvalid   = errors.New("webhook input is invalid")
	ErrNotFound  = errors.New("webhook resource was not found")
	ErrForbidden = errors.New("webhook operation is not permitted")
	ErrConflict  = errors.New("webhook already exists")
)

var eventKinds = map[string]struct{}{
	"actions":       {},
	"branch_rules":  {},
	"branches":      {},
	"comments":      {},
	"issues":        {},
	"labels":        {},
	"milestones":    {},
	"projects":      {},
	"pull_requests": {},
	"releases":      {},
	"repository":    {},
	"reviews":       {},
	"wiki":          {},
}

type Webhook struct {
	ID               string    `json:"id"`
	URL              string    `json:"url"`
	Events           []string  `json:"events"`
	Active           bool      `json:"active"`
	SecretConfigured bool      `json:"secretConfigured"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type CreateInput struct {
	URL    string   `json:"url"`
	Events []string `json:"events"`
	Active *bool    `json:"active,omitempty"`
	Secret string   `json:"secret"`
}

type UpdateInput struct {
	URL    *string   `json:"url,omitempty"`
	Events *[]string `json:"events,omitempty"`
	Active *bool     `json:"active,omitempty"`
	Secret *string   `json:"secret,omitempty"`
}

type Delivery struct {
	ID             string     `json:"id"`
	Event          string     `json:"event"`
	Status         string     `json:"status"`
	AttemptCount   int        `json:"attemptCount"`
	ResponseStatus *int       `json:"responseStatus,omitempty"`
	ResponseBody   string     `json:"responseBody"`
	LastError      string     `json:"lastError"`
	DeliveredAt    *time.Time `json:"deliveredAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type DeliveryAttempt struct {
	AttemptNumber  int       `json:"attemptNumber"`
	StartedAt      time.Time `json:"startedAt"`
	FinishedAt     time.Time `json:"finishedAt"`
	ResponseStatus *int      `json:"responseStatus,omitempty"`
	ResponseBody   string    `json:"responseBody"`
	ErrorMessage   string    `json:"errorMessage"`
}

type DeliveryDetail struct {
	Delivery Delivery          `json:"delivery"`
	Attempts []DeliveryAttempt `json:"attempts"`
}

type repositoryRef struct {
	ID             string
	OrganizationID string
	Owner          string
	Slug           string
}

func normalizeEvents(events []string) ([]string, error) {
	if len(events) == 0 || len(events) > len(eventKinds) {
		return nil, invalid("at least one supported event is required")
	}
	seen := make(map[string]struct{}, len(events))
	normalized := make([]string, 0, len(events))
	for _, event := range events {
		event = strings.TrimSpace(event)
		if _, supported := eventKinds[event]; !supported {
			return nil, invalid("unsupported event")
		}
		if _, duplicate := seen[event]; duplicate {
			return nil, invalid("events must be unique")
		}
		seen[event] = struct{}{}
		normalized = append(normalized, event)
	}
	sort.Strings(normalized)
	return normalized, nil
}

func validateSecret(secret string) error {
	if len(secret) < 16 || len(secret) > 512 || strings.ContainsRune(secret, '\x00') {
		return invalid("secret must contain between 16 and 512 bytes")
	}
	return nil
}

func invalid(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalid, message)
}

func eventKind(topic string) (string, bool) {
	switch {
	case strings.HasPrefix(topic, "actions."):
		return "actions", true
	case strings.HasPrefix(topic, "branch_rule."):
		return "branch_rules", true
	case strings.HasPrefix(topic, "branch."):
		return "branches", true
	case strings.HasPrefix(topic, "issue_comment."), strings.HasPrefix(topic, "merge_request_comment."):
		return "comments", true
	case strings.HasPrefix(topic, "issue_label."), strings.HasPrefix(topic, "issue."):
		return "issues", true
	case strings.HasPrefix(topic, "label."):
		return "labels", true
	case strings.HasPrefix(topic, "milestone."):
		return "milestones", true
	case strings.HasPrefix(topic, "project."):
		return "projects", true
	case strings.HasPrefix(topic, "merge_request_review_request."),
		strings.HasPrefix(topic, "merge_request_review."),
		strings.HasPrefix(topic, "merge_request_review_thread."),
		strings.HasPrefix(topic, "merge_request_review_comment."):
		return "reviews", true
	case strings.HasPrefix(topic, "merge_request."), strings.HasPrefix(topic, "merge_operation."):
		return "pull_requests", true
	case strings.HasPrefix(topic, "release."):
		return "releases", true
	case strings.HasPrefix(topic, "wiki."):
		return "wiki", true
	case strings.HasPrefix(topic, "repository."):
		return "repository", true
	default:
		return "", false
	}
}
