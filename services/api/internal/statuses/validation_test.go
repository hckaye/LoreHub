package statuses

import (
	"strings"
	"testing"
)

const testRevision = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestValidateCreateNormalizesOptionalFields(t *testing.T) {
	key := " delivery-1 "
	input, err := validateCreate(CreateInput{
		Revision: testRevision, State: " success ", IdempotencyKey: &key,
	})
	if err != nil {
		t.Fatalf("validateCreate returned %v", err)
	}
	if input.Context != "default" || input.State != "success" {
		t.Fatalf("normalized input = %#v", input)
	}
	if input.IdempotencyKey == nil || *input.IdempotencyKey != "delivery-1" {
		t.Fatalf("idempotency key = %#v", input.IdempotencyKey)
	}
}

func TestValidateCreateRejectsInvalidValues(t *testing.T) {
	longContext := strings.Repeat("x", maxContextRunes+1)
	longDescription := strings.Repeat("x", maxDescriptionRunes+1)
	longKey := strings.Repeat("x", maxIdempotencyRunes+1)
	tests := []CreateInput{
		{Revision: "bad", State: "success"},
		{Revision: strings.ToUpper(testRevision), State: "success"},
		{Revision: testRevision, Context: longContext, State: "success"},
		{Revision: testRevision, Context: "ci\ntest", State: "success"},
		{Revision: testRevision, State: "complete"},
		{Revision: testRevision, State: "success", Description: longDescription},
		{Revision: testRevision, State: "success", TargetURL: "javascript:alert(1)"},
		{Revision: testRevision, State: "success", TargetURL: "https://user:pass@example.com"},
		{Revision: testRevision, State: "success", IdempotencyKey: &longKey},
	}
	for index, input := range tests {
		if _, err := validateCreate(input); err == nil {
			t.Errorf("case %d accepted %#v", index, input)
		}
	}
}

func TestCombinedStatePrecedence(t *testing.T) {
	tests := []struct {
		statuses []Status
		want     string
	}{
		{nil, "pending"},
		{[]Status{{State: "success"}}, "success"},
		{[]Status{{State: "success"}, {State: "pending"}}, "pending"},
		{[]Status{{State: "pending"}, {State: "failure"}}, "failure"},
		{[]Status{{State: "success"}, {State: "error"}}, "failure"},
	}
	for _, test := range tests {
		if got := combinedState(test.statuses); got != test.want {
			t.Errorf("combinedState(%#v) = %q, want %q", test.statuses, got, test.want)
		}
	}
}

func TestWithIdempotencyHeaderRejectsMismatch(t *testing.T) {
	key := "body-key"
	_, err := withIdempotencyHeader(CreateInput{IdempotencyKey: &key}, "header-key")
	if err == nil {
		t.Fatal("mismatched idempotency keys were accepted")
	}
}
