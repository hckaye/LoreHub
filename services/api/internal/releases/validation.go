package releases

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
)

const (
	maxTagRunes       = 128
	maxTitleRunes     = 512
	maxNotesRunes     = 1_048_576
	maxAssetNameRunes = 255
	maxAssetURLBytes  = 8_192
)

func validateCreate(input CreateInput) (CreateInput, error) {
	input.TagName = strings.TrimSpace(input.TagName)
	input.Title = strings.TrimSpace(input.Title)
	input.Notes = strings.TrimSpace(input.Notes)
	input.SourceBranch = strings.TrimSpace(input.SourceBranch)
	input.Revision = strings.TrimSpace(input.Revision)
	input.State = strings.TrimSpace(input.State)
	if !validTagName(input.TagName) {
		return CreateInput{}, invalid("tagName is invalid")
	}
	if !validText(input.Title, 1, maxTitleRunes, false) {
		return CreateInput{}, invalid("title is required and must not exceed 512 characters")
	}
	if !validText(input.Notes, 0, maxNotesRunes, true) {
		return CreateInput{}, invalid("notes must not exceed 1048576 characters")
	}
	if !loreclient.ValidBranchName(input.SourceBranch) {
		return CreateInput{}, invalid("sourceBranch is invalid")
	}
	if !validRevision(input.Revision) {
		return CreateInput{}, invalid("revision must be one lowercase Lore revision")
	}
	if input.State == "" {
		input.State = "draft"
	}
	if input.State != "draft" && input.State != "published" {
		return CreateInput{}, invalid("state must be draft or published")
	}
	return input, nil
}

func validateUpdate(input UpdateInput) (UpdateInput, error) {
	if input.Title == nil && input.Notes == nil {
		return UpdateInput{}, invalid("at least one release field is required")
	}
	if input.ExpectedVersion < 1 {
		return UpdateInput{}, invalid("expectedVersion must be positive")
	}
	if input.Title != nil {
		value := strings.TrimSpace(*input.Title)
		if !validText(value, 1, maxTitleRunes, false) {
			return UpdateInput{}, invalid("title is required and must not exceed 512 characters")
		}
		input.Title = &value
	}
	if input.Notes != nil {
		value := strings.TrimSpace(*input.Notes)
		if !validText(value, 0, maxNotesRunes, true) {
			return UpdateInput{}, invalid("notes must not exceed 1048576 characters")
		}
		input.Notes = &value
	}
	return input, nil
}

func validateAsset(input AssetInput) (AssetInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.ExternalURL = strings.TrimSpace(input.ExternalURL)
	if !validText(input.Name, 1, maxAssetNameRunes, false) {
		return AssetInput{}, invalid("asset name is required and must not exceed 255 characters")
	}
	if input.ExpectedVersion < 1 {
		return AssetInput{}, invalid("expectedVersion must be positive")
	}
	if len(input.ExternalURL) > maxAssetURLBytes || hasControl(input.ExternalURL) {
		return AssetInput{}, invalid("externalUrl is invalid")
	}
	parsed, err := url.ParseRequestURI(input.ExternalURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" || parsed.User != nil {
		return AssetInput{}, invalid("externalUrl must be an HTTP or HTTPS URL without credentials")
	}
	return input, nil
}

func validTagName(value string) bool {
	if !validText(value, 1, maxTagRunes, false) || strings.HasPrefix(value, "/") ||
		strings.HasSuffix(value, "/") || strings.Contains(value, "//") || strings.Contains(value, "..") {
		return false
	}
	for _, character := range value {
		if unicode.IsLetter(character) || unicode.IsDigit(character) ||
			strings.ContainsRune("-._/+", character) {
			continue
		}
		return false
	}
	return true
}

func validRevision(value string) bool {
	return len(value) == 64 && strings.Trim(value, "0123456789abcdef") == ""
}

func validText(value string, minimum, maximum int, allowNewline bool) bool {
	if !utf8.ValidString(value) {
		return false
	}
	length := utf8.RuneCountInString(value)
	if length < minimum || length > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) && (!allowNewline || (character != '\n' && character != '\t')) {
			return false
		}
	}
	return true
}

func hasControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) || unicode.IsSpace(character) {
			return true
		}
	}
	return false
}

func invalid(detail string) error {
	return fmt.Errorf("%w: %s", ErrInvalidInput, detail)
}
