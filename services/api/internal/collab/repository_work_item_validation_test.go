package collab

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeRepositoryIssueQueryDefaultsAndLabels(t *testing.T) {
	query, err := NormalizeRepositoryIssueQuery(RepositoryIssueQuery{
		Search: "  renderer  ", Labels: []string{"Bug", " bug ", "Help wanted"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if query.State != "open" || query.Search != "renderer" || query.Sort != "updated" ||
		query.Direction != "desc" || query.Page != 1 || query.PerPage != 25 {
		t.Fatalf("normalized query = %#v", query)
	}
	if len(query.Labels) != 2 || query.Labels[0] != "Bug" || query.Labels[1] != "Help wanted" {
		t.Fatalf("normalized labels = %#v", query.Labels)
	}
}

func TestNormalizeRepositoryWorkItemQueryRejectsInvalidValues(t *testing.T) {
	tests := []RepositoryIssueQuery{
		{State: "merged"},
		{Search: "line\nbreak"},
		{Labels: []string{""}},
		{Labels: make([]string, 21)},
		{MilestoneNumber: pointerToInt64(1), WithoutMilestone: true},
		{Sort: "relevance"},
		{Direction: "sideways"},
		{Page: 10_001},
		{PerPage: 101},
	}
	for _, query := range tests {
		if _, err := NormalizeRepositoryIssueQuery(query); !errors.Is(
			err, ErrInvalidRepositoryWorkItemQuery,
		) {
			t.Fatalf("query %#v error = %v", query, err)
		}
	}
}

func TestNormalizeRepositoryMergeRequestQuery(t *testing.T) {
	draft := true
	query, err := NormalizeRepositoryMergeRequestQuery(RepositoryMergeRequestQuery{
		State: "all", SourceBranch: " feature/render ", TargetBranch: "main",
		Draft: &draft, Sort: "comments", Direction: "asc", Page: 2, PerPage: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if query.SourceBranch != "feature/render" || query.TargetBranch != "main" ||
		query.Sort != "comments" || query.Direction != "asc" || query.Draft == nil || !*query.Draft {
		t.Fatalf("normalized pull request query = %#v", query)
	}
	query.SourceBranch = strings.Repeat("a", 256)
	if _, err := NormalizeRepositoryMergeRequestQuery(query); !errors.Is(
		err, ErrInvalidRepositoryWorkItemQuery,
	) {
		t.Fatalf("long branch error = %v", err)
	}
}

func pointerToInt64(value int64) *int64 {
	return &value
}
