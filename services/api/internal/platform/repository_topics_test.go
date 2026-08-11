package platform

import (
	"errors"
	"slices"
	"testing"
)

func TestNormalizeRepositoryTopics(t *testing.T) {
	values := []string{" Lore ", "ci-runner", "lore"}
	topics, err := normalizeRepositoryTopics(&values)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(*topics, []string{"ci-runner", "lore"}) {
		t.Fatalf("normalized topics = %v", *topics)
	}
	if topics, err := normalizeRepositoryTopics(nil); err != nil || topics != nil {
		t.Fatalf("omitted topics = %v, %v", topics, err)
	}
	for _, values := range [][]string{{"bad_topic"}, {""}, repositoryTopicFixture(21)} {
		if _, err := normalizeRepositoryTopics(&values); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("normalize topics %v error = %v", values, err)
		}
	}
}

func repositoryTopicFixture(count int) []string {
	values := make([]string, count)
	for index := range values {
		values[index] = "topic-" + string(rune('a'+index))
	}
	return values
}
