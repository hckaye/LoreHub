package milestones

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	maxTitleRunes       = 255
	maxDescriptionRunes = 65_536
	dueDateLayout       = "2006-01-02"
)

func validateCreate(input CreateInput) (CreateInput, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	if !validText(input.Title, 1, maxTitleRunes, false) {
		return CreateInput{}, invalid("title is required and must not exceed 255 characters")
	}
	if !validText(input.Description, 0, maxDescriptionRunes, true) {
		return CreateInput{}, invalid("description must not exceed 65536 characters")
	}
	dueOn, err := validateDueOn(input.DueOn)
	if err != nil {
		return CreateInput{}, err
	}
	input.DueOn = dueOn
	return input, nil
}

func validateUpdate(input UpdateInput) (UpdateInput, error) {
	if input.Title == nil && input.Description == nil && input.State == nil && !input.DueOnSet {
		return UpdateInput{}, invalid("at least one milestone field is required")
	}
	if input.ExpectedVersion < 1 {
		return UpdateInput{}, invalid("expectedVersion must be positive")
	}
	if input.Title != nil {
		value := strings.TrimSpace(*input.Title)
		if !validText(value, 1, maxTitleRunes, false) {
			return UpdateInput{}, invalid("title is required and must not exceed 255 characters")
		}
		input.Title = &value
	}
	if input.Description != nil {
		value := strings.TrimSpace(*input.Description)
		if !validText(value, 0, maxDescriptionRunes, true) {
			return UpdateInput{}, invalid("description must not exceed 65536 characters")
		}
		input.Description = &value
	}
	if input.State != nil {
		value := strings.TrimSpace(*input.State)
		if value != "open" && value != "closed" {
			return UpdateInput{}, invalid("state must be open or closed")
		}
		input.State = &value
	}
	if input.DueOnSet {
		dueOn, err := validateDueOn(input.DueOn)
		if err != nil {
			return UpdateInput{}, err
		}
		input.DueOn = dueOn
	}
	return input, nil
}

func validateDueOn(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	normalized := strings.TrimSpace(*value)
	parsed, err := time.Parse(dueDateLayout, normalized)
	if err != nil || parsed.Format(dueDateLayout) != normalized {
		return nil, invalid("dueOn must be a calendar date in YYYY-MM-DD format")
	}
	return &normalized, nil
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

func invalid(detail string) error {
	return fmt.Errorf("%w: %s", ErrInvalidInput, detail)
}
