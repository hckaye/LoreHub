package collab

import "testing"

func TestRequiredBranchStatusChecksUsesCaseInsensitiveUnion(t *testing.T) {
	rules := []BranchRule{
		{RequiredStatusChecks: []string{"CI/Test", "Lint"}},
		{RequiredStatusChecks: []string{"build", "ci/test"}},
	}
	checks := RequiredBranchStatusChecks(rules)
	if len(checks) != 3 || checks[0] != "build" || checks[1] != "CI/Test" || checks[2] != "Lint" {
		t.Fatalf("required status checks = %#v, want stable case-insensitive union", checks)
	}
}

func TestRequiredBranchStatusChecksSuccessful(t *testing.T) {
	required := []string{"CI/Test", "lint"}
	checks := []RevisionStatusCheck{
		{Context: "ci/test", State: "success"},
		{Context: "LINT", State: "success"},
	}
	if !RequiredBranchStatusChecksSuccessful(required, checks) {
		t.Fatal("case-insensitive successful checks were rejected")
	}
	checks[1].State = "pending"
	if RequiredBranchStatusChecksSuccessful(required, checks) {
		t.Fatal("pending required check was accepted")
	}
	if RequiredBranchStatusChecksSuccessful(append(required, "missing"), checks) {
		t.Fatal("missing required check was accepted")
	}
}
