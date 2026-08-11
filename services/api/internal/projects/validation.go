package projects

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	maxTitleRunes       = 512
	maxDescriptionRunes = 65_536
	maxColumnNameRunes  = 255
	maxDraftBodyRunes   = 65_536
)

func validateProjectInput(input ProjectInput) (ProjectInput, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	input.State = strings.TrimSpace(input.State)
	if input.Title == "" || utf8.RuneCountInString(input.Title) > maxTitleRunes {
		return ProjectInput{}, fmt.Errorf("%w: project title is required and must not exceed 512 characters",
			ErrInvalidInput)
	}
	if utf8.RuneCountInString(input.Description) > maxDescriptionRunes {
		return ProjectInput{}, fmt.Errorf("%w: project description must not exceed 65536 characters",
			ErrInvalidInput)
	}
	if input.State == "" {
		input.State = "open"
	}
	if input.State != "open" && input.State != "closed" {
		return ProjectInput{}, fmt.Errorf("%w: project state must be open or closed", ErrInvalidInput)
	}
	return input, nil
}

func validateProjectUpdate(input ProjectUpdate) (ProjectUpdate, error) {
	if input.Title == nil && input.Description == nil && input.State == nil {
		return ProjectUpdate{}, fmt.Errorf("%w: at least one project field is required", ErrInvalidInput)
	}
	if input.Title != nil {
		value := strings.TrimSpace(*input.Title)
		if value == "" || utf8.RuneCountInString(value) > maxTitleRunes {
			return ProjectUpdate{}, fmt.Errorf(
				"%w: project title is required and must not exceed 512 characters", ErrInvalidInput,
			)
		}
		input.Title = &value
	}
	if input.Description != nil {
		value := strings.TrimSpace(*input.Description)
		if utf8.RuneCountInString(value) > maxDescriptionRunes {
			return ProjectUpdate{}, fmt.Errorf(
				"%w: project description must not exceed 65536 characters", ErrInvalidInput,
			)
		}
		input.Description = &value
	}
	if input.State != nil {
		value := strings.TrimSpace(*input.State)
		if value != "open" && value != "closed" {
			return ProjectUpdate{}, fmt.Errorf("%w: project state must be open or closed", ErrInvalidInput)
		}
		input.State = &value
	}
	return input, nil
}

func validateColumnInput(input ColumnInput) (ColumnInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || utf8.RuneCountInString(input.Name) > maxColumnNameRunes {
		return ColumnInput{}, fmt.Errorf("%w: column name is required and must not exceed 255 characters",
			ErrInvalidInput)
	}
	return input, nil
}

func validateItemInput(input ItemInput) (ItemInput, error) {
	if _, err := uuid.Parse(input.ColumnID); err != nil {
		return ItemInput{}, fmt.Errorf("%w: columnId must be a UUID", ErrInvalidInput)
	}
	input.Kind = strings.TrimSpace(input.Kind)
	input.Title = strings.TrimSpace(input.Title)
	input.Body = strings.TrimSpace(input.Body)
	switch input.Kind {
	case "issue":
		if input.IssueNumber == nil || *input.IssueNumber < 1 || input.MergeRequestNumber != nil ||
			input.Title != "" || input.Body != "" {
			return ItemInput{}, fmt.Errorf("%w: issue cards require only a positive issueNumber", ErrInvalidInput)
		}
	case "merge_request":
		if input.MergeRequestNumber == nil || *input.MergeRequestNumber < 1 || input.IssueNumber != nil ||
			input.Title != "" || input.Body != "" {
			return ItemInput{}, fmt.Errorf("%w: pull request cards require only a positive mergeRequestNumber",
				ErrInvalidInput)
		}
	case "draft":
		if input.IssueNumber != nil || input.MergeRequestNumber != nil || input.Title == "" ||
			utf8.RuneCountInString(input.Title) > maxTitleRunes ||
			utf8.RuneCountInString(input.Body) > maxDraftBodyRunes {
			return ItemInput{}, fmt.Errorf("%w: draft cards require a title of at most 512 characters",
				ErrInvalidInput)
		}
	default:
		return ItemInput{}, fmt.Errorf("%w: card kind must be issue, merge_request or draft", ErrInvalidInput)
	}
	return input, nil
}

func validateItemUpdate(input ItemUpdate) (ItemUpdate, error) {
	if input.ColumnID == nil && input.Title == nil && input.Body == nil {
		return ItemUpdate{}, fmt.Errorf("%w: at least one card field is required", ErrInvalidInput)
	}
	if input.ColumnID != nil {
		value := strings.TrimSpace(*input.ColumnID)
		if _, err := uuid.Parse(value); err != nil {
			return ItemUpdate{}, fmt.Errorf("%w: columnId must be a UUID", ErrInvalidInput)
		}
		input.ColumnID = &value
	}
	if input.Title != nil {
		value := strings.TrimSpace(*input.Title)
		if value == "" || utf8.RuneCountInString(value) > maxTitleRunes {
			return ItemUpdate{}, fmt.Errorf("%w: draft title is required and must not exceed 512 characters",
				ErrInvalidInput)
		}
		input.Title = &value
	}
	if input.Body != nil {
		value := strings.TrimSpace(*input.Body)
		if utf8.RuneCountInString(value) > maxDraftBodyRunes {
			return ItemUpdate{}, fmt.Errorf("%w: draft body must not exceed 65536 characters", ErrInvalidInput)
		}
		input.Body = &value
	}
	return input, nil
}
