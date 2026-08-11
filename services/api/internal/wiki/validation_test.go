package wiki

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeCreate(t *testing.T) {
	tests := []struct {
		name string
		in   CreateInput
		slug string
	}{
		{name: "english title", in: CreateInput{Title: "Release Guide"}, slug: "release-guide"},
		{name: "japanese title", in: CreateInput{Title: "リリース 手順"}, slug: "リリース-手順"},
		{name: "explicit slug", in: CreateInput{Slug: "API_Notes", Title: "API"}, slug: "api-notes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			normalized, err := normalizeCreate(test.in)
			if err != nil {
				t.Fatalf("normalize create: %v", err)
			}
			if normalized.Slug != test.slug {
				t.Fatalf("slug = %q, want %q", normalized.Slug, test.slug)
			}
		})
	}
}

func TestNormalizeCreateRejectsInvalidInput(t *testing.T) {
	tests := []CreateInput{
		{},
		{Title: strings.Repeat("x", maxTitleBytes+1)},
		{Title: "Page", Body: strings.Repeat("x", maxBodyBytes+1)},
		{Title: "Page", EditSummary: strings.Repeat("x", maxSummaryBytes+1)},
		{Slug: strings.Repeat("x", maxSlugBytes+1), Title: "Page"},
	}
	for index, input := range tests {
		if _, err := normalizeCreate(input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d error = %v, want invalid input", index, err)
		}
	}
}

func TestNormalizeUpdatePreservesOmittedFields(t *testing.T) {
	page := Page{PageSummary: PageSummary{Slug: "guide", Title: "Guide"}, Body: "old"}
	summary := "Clarify setup"
	normalized, err := normalizeUpdate(page, UpdateInput{EditSummary: summary})
	if err != nil {
		t.Fatalf("normalize update: %v", err)
	}
	if normalized.Slug != page.Slug || normalized.Title != page.Title || normalized.Body != page.Body {
		t.Fatalf("normalized update = %+v", normalized)
	}
	if normalized.EditSummary != summary {
		t.Fatalf("summary = %q, want %q", normalized.EditSummary, summary)
	}
}
