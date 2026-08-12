package discussions

import (
	"errors"
	"strings"
	"testing"
)

func TestDiscussionTextLimitsCountCharactersForLocalizedText(t *testing.T) {
	if _, err := normalizeCreateInput(CreateInput{
		CategorySlug: "general",
		Title:        strings.Repeat("あ", maxDiscussionTitleCharacters),
	}); err != nil {
		t.Fatalf("localized title at character limit: %v", err)
	}
	if _, err := normalizeCreateInput(CreateInput{
		CategorySlug: "general",
		Title:        strings.Repeat("あ", maxDiscussionTitleCharacters+1),
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("localized title above character limit = %v", err)
	}
	if _, err := normalizeCategoryInput(CategoryInput{
		Slug:        "localized",
		Name:        strings.Repeat("界", maxCategoryNameCharacters),
		Description: strings.Repeat("説", maxCategoryTextCharacters),
		Format:      "discussion",
	}); err != nil {
		t.Fatalf("localized category at character limits: %v", err)
	}
}

func TestDiscussionSearchPatternEscapesWildcards(t *testing.T) {
	if pattern := discussionSearchPattern(`50%_\complete`); pattern != `%50\%\_\\complete%` {
		t.Fatalf("search pattern = %q", pattern)
	}
	if pattern := discussionSearchPattern(""); pattern != "" {
		t.Fatalf("empty search pattern = %q", pattern)
	}
}
