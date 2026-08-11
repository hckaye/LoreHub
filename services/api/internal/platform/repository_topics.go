package platform

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

const maxRepositoryTopics = 20

var repositoryTopicPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func normalizeRepositoryTopics(values *[]string) (*[]string, error) {
	if values == nil {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(*values))
	topics := make([]string, 0, len(*values))
	for _, value := range *values {
		topic := strings.ToLower(strings.TrimSpace(value))
		if len(topic) > 50 || !repositoryTopicPattern.MatchString(topic) {
			return nil, ErrInvalidInput
		}
		if _, exists := seen[topic]; exists {
			continue
		}
		seen[topic] = struct{}{}
		topics = append(topics, topic)
	}
	if len(topics) > maxRepositoryTopics {
		return nil, ErrInvalidInput
	}
	sort.Strings(topics)
	return &topics, nil
}

func replaceRepositoryTopics(
	ctx context.Context,
	transaction pgx.Tx,
	repositoryID string,
	actorID string,
	topics []string,
) error {
	if _, err := transaction.Exec(ctx, `
		DELETE FROM repository_topics WHERE repository_id = $1
	`, repositoryID); err != nil {
		return fmt.Errorf("clear repository topics: %w", err)
	}
	for _, topic := range topics {
		if _, err := transaction.Exec(ctx, `
			INSERT INTO repository_topics (repository_id, topic, created_by)
			VALUES ($1, $2, $3)
		`, repositoryID, topic, actorID); err != nil {
			return translateConstraintError("save repository topic", err)
		}
	}
	return nil
}
