package wiki

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxSlugBytes    = 160
	maxTitleBytes   = 256
	maxBodyBytes    = 1 << 20
	maxSummaryBytes = 256
)

func normalizeCreate(input CreateInput) (CreateInput, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.EditSummary = strings.TrimSpace(input.EditSummary)
	slugSource := input.Slug
	if strings.TrimSpace(slugSource) == "" {
		slugSource = input.Title
	}
	input.Slug = slugify(slugSource)
	if err := validatePage(input.Slug, input.Title, input.Body, input.EditSummary); err != nil {
		return CreateInput{}, err
	}
	return input, nil
}

func normalizeUpdate(current Page, input UpdateInput) (CreateInput, error) {
	result := CreateInput{
		Slug: current.Slug, Title: current.Title, Body: current.Body,
		EditSummary: strings.TrimSpace(input.EditSummary),
	}
	if input.Slug != nil {
		result.Slug = slugify(*input.Slug)
	}
	if input.Title != nil {
		result.Title = strings.TrimSpace(*input.Title)
	}
	if input.Body != nil {
		result.Body = *input.Body
	}
	if err := validatePage(result.Slug, result.Title, result.Body, result.EditSummary); err != nil {
		return CreateInput{}, err
	}
	return result, nil
}

func validatePage(slug string, title string, body string, summary string) error {
	if slug == "" || len(slug) > maxSlugBytes || !utf8.ValidString(slug) {
		return invalid("slug must contain between 1 and 160 bytes")
	}
	if title == "" || len(title) > maxTitleBytes || !utf8.ValidString(title) {
		return invalid("title must contain between 1 and 256 bytes")
	}
	if len(body) > maxBodyBytes || !utf8.ValidString(body) {
		return invalid("body must not exceed 1 MiB")
	}
	if len(summary) > maxSummaryBytes || !utf8.ValidString(summary) {
		return invalid("edit summary must not exceed 256 bytes")
	}
	return nil
}

func slugify(value string) string {
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
