package projects

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestValidateItemInput(t *testing.T) {
	columnID := uuid.NewString()
	issueNumber := int64(12)
	mergeRequestNumber := int64(7)
	tests := []struct {
		name  string
		input ItemInput
		valid bool
	}{
		{name: "issue", input: ItemInput{ColumnID: columnID, Kind: "issue", IssueNumber: &issueNumber}, valid: true},
		{
			name:  "pull request",
			input: ItemInput{ColumnID: columnID, Kind: "merge_request", MergeRequestNumber: &mergeRequestNumber},
			valid: true,
		},
		{name: "draft", input: ItemInput{ColumnID: columnID, Kind: "draft", Title: "Plan release"}, valid: true},
		{name: "missing issue", input: ItemInput{ColumnID: columnID, Kind: "issue"}},
		{
			name: "mixed content",
			input: ItemInput{
				ColumnID: columnID, Kind: "issue", IssueNumber: &issueNumber, MergeRequestNumber: &mergeRequestNumber,
			},
		},
		{name: "invalid column", input: ItemInput{ColumnID: "not-a-uuid", Kind: "draft", Title: "Plan"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := validateItemInput(test.input)
			if test.valid && err != nil {
				t.Fatalf("validate item: %v", err)
			}
			if !test.valid && !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestValidateProjectInputDefaultsOpenState(t *testing.T) {
	input, err := validateProjectInput(ProjectInput{Title: "  Release board  "})
	if err != nil {
		t.Fatalf("validate project: %v", err)
	}
	if input.Title != "Release board" || input.State != "open" {
		t.Fatalf("validated input = %+v", input)
	}
}
