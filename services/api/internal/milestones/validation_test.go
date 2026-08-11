package milestones

import "testing"

func TestValidateMilestoneInput(t *testing.T) {
	dueOn := "2026-12-31"
	created, err := validateCreate(CreateInput{
		Title: " Version 2 ", Description: " Scope ", DueOn: &dueOn,
	})
	if err != nil || created.Title != "Version 2" || created.DueOn == nil || *created.DueOn != dueOn {
		t.Fatalf("valid create = %#v, error = %v", created, err)
	}
	for _, input := range []CreateInput{
		{},
		{Title: "Bad\x00title"},
		{Title: "Release", DueOn: stringPointer("2026-02-30")},
		{Title: "Release", DueOn: stringPointer("31-12-2026")},
	} {
		if _, err := validateCreate(input); err == nil {
			t.Fatalf("invalid create accepted: %#v", input)
		}
	}
	title := " Updated "
	state := "closed"
	updated, err := validateUpdate(UpdateInput{
		Title: &title, State: &state, DueOnSet: true, ExpectedVersion: 2,
	})
	if err != nil || updated.Title == nil || *updated.Title != "Updated" || updated.DueOn != nil {
		t.Fatalf("valid update = %#v, error = %v", updated, err)
	}
	if _, err := validateUpdate(UpdateInput{ExpectedVersion: 1}); err == nil {
		t.Fatal("empty update was accepted")
	}
}

func stringPointer(value string) *string {
	return &value
}
