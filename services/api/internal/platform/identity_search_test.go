package platform

import (
	"math"
	"strings"
	"testing"
)

func TestValidateSearchInput(t *testing.T) {
	offset, err := validateSearchInput("needle", "all", 2, 20)
	if err != nil || offset != 20 {
		t.Fatalf("offset = %d, error = %v", offset, err)
	}
	invalid := []struct {
		query   string
		kind    string
		page    int
		perPage int
	}{
		{"", "all", 1, 20},
		{"nul\x00query", "all", 1, 20},
		{"line\nquery", "all", 1, 20},
		{"control\u0085query", "all", 1, 20},
		{strings.Repeat("界", 161), "all", 1, 20},
		{"x", "unknown", 1, 20},
		{"x", "all", 0, 20},
		{"x", "all", maxSearchPage + 1, 20},
		{"x", "all", 1, 0},
		{"x", "all", 1, maxSearchPerPage + 1},
		{"x", "all", math.MaxInt, math.MaxInt},
	}
	for _, input := range invalid {
		if _, err := validateSearchInput(input.query, input.kind, input.page, input.perPage); err == nil {
			t.Errorf("accepted invalid input %#v", input)
		}
	}
}

func TestEmptySearchResultsUsesJSONArrays(t *testing.T) {
	result := emptySearchResults(3, 7)
	if result.Repositories == nil || result.Organizations == nil || result.Users == nil ||
		result.Issues == nil || result.PullRequests == nil {
		t.Fatalf("search arrays contain nil: %#v", result)
	}
	if result.Page != 3 || result.PerPage != 7 {
		t.Fatalf("paging = %d/%d", result.Page, result.PerPage)
	}
}

func TestValidSearchKind(t *testing.T) {
	for _, kind := range []string{"all", "repositories", "organizations", "users", "issues", "pulls"} {
		if !validSearchKind(kind) {
			t.Errorf("valid kind %q rejected", kind)
		}
	}
	if validSearchKind("pull_requests") {
		t.Fatal("unknown kind accepted")
	}
}
