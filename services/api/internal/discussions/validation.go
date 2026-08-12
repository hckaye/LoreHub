package discussions

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxDiscussionTitleCharacters = 512
	maxDiscussionBodyBytes       = 1 << 20
	maxCommentBodyBytes          = 256 << 10
	maxCategoryNameCharacters    = 100
	maxCategoryTextCharacters    = 500
)

var categorySlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func normalizeCreateInput(input CreateInput) (CreateInput, error) {
	input.CategorySlug = normalizeCategorySlug(input.CategorySlug)
	input.Title = strings.TrimSpace(input.Title)
	if !categorySlugPattern.MatchString(input.CategorySlug) {
		return CreateInput{}, invalid("category is invalid")
	}
	if err := validateTitleAndBody(input.Title, input.Body); err != nil {
		return CreateInput{}, err
	}
	return input, nil
}

func normalizeUpdateInput(input UpdateInput) (UpdateInput, error) {
	if input.CategorySlug != nil {
		value := normalizeCategorySlug(*input.CategorySlug)
		if !categorySlugPattern.MatchString(value) {
			return UpdateInput{}, invalid("category is invalid")
		}
		input.CategorySlug = &value
	}
	if input.Title != nil {
		value := strings.TrimSpace(*input.Title)
		if value == "" || utf8.RuneCountInString(value) > maxDiscussionTitleCharacters ||
			!utf8.ValidString(value) {
			return UpdateInput{}, invalid("title must contain between 1 and 512 characters")
		}
		input.Title = &value
	}
	if input.Body != nil && (len(*input.Body) > maxDiscussionBodyBytes || !utf8.ValidString(*input.Body)) {
		return UpdateInput{}, invalid("body must not exceed 1 MiB")
	}
	if input.State != nil && *input.State != "open" && *input.State != "closed" {
		return UpdateInput{}, invalid("state must be open or closed")
	}
	if input.CategorySlug == nil && input.Title == nil && input.Body == nil && input.State == nil &&
		input.Locked == nil && input.Pinned == nil {
		return UpdateInput{}, invalid("at least one field is required")
	}
	return input, nil
}

func normalizeCategoryInput(input CategoryInput) (CategoryInput, error) {
	input.Slug = normalizeCategorySlug(input.Slug)
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.Format = strings.ToLower(strings.TrimSpace(input.Format))
	if !categorySlugPattern.MatchString(input.Slug) {
		return CategoryInput{}, invalid("category slug is invalid")
	}
	if input.Name == "" || utf8.RuneCountInString(input.Name) > maxCategoryNameCharacters ||
		!utf8.ValidString(input.Name) {
		return CategoryInput{}, invalid("category name must contain between 1 and 100 characters")
	}
	if utf8.RuneCountInString(input.Description) > maxCategoryTextCharacters ||
		!utf8.ValidString(input.Description) {
		return CategoryInput{}, invalid("category description must not exceed 500 characters")
	}
	if input.Format != "discussion" && input.Format != "question" && input.Format != "announcement" {
		return CategoryInput{}, invalid("category format is invalid")
	}
	return input, nil
}

func normalizeCommentBody(body string) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" || len(body) > maxCommentBodyBytes || !utf8.ValidString(body) {
		return "", invalid("comment must contain between 1 and 262144 bytes")
	}
	return body, nil
}

func validateTitleAndBody(title string, body string) error {
	if title == "" || utf8.RuneCountInString(title) > maxDiscussionTitleCharacters ||
		!utf8.ValidString(title) {
		return invalid("title must contain between 1 and 512 characters")
	}
	if len(body) > maxDiscussionBodyBytes || !utf8.ValidString(body) {
		return invalid("body must not exceed 1 MiB")
	}
	return nil
}

func normalizeCategorySlug(value string) string {
	var builder strings.Builder
	pendingDash := false
	for _, character := range strings.ToLower(strings.TrimSpace(value)) {
		switch {
		case unicode.IsLetter(character), unicode.IsNumber(character):
			if pendingDash && builder.Len() > 0 {
				builder.WriteByte('-')
			}
			pendingDash = false
			builder.WriteRune(character)
		case character == '-' || character == '_' || unicode.IsSpace(character):
			pendingDash = builder.Len() > 0
		}
	}
	return builder.String()
}

func invalid(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidInput, message)
}
