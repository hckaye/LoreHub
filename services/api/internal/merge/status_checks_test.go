package merge

import (
	"strings"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/collab"
)

func TestEvaluateRequiredStatusChecks(t *testing.T) {
	checks := []collab.RevisionStatusCheck{
		{Context: "optional", State: "failure"},
		{Context: "CI/Test", State: "success"},
		{Context: "lint", State: "pending"},
		{Context: "security", State: "error"},
	}
	result, unsuccessful := evaluateRequiredStatusChecks(
		checks,
		[]string{"ci/test", "lint", "missing", "security"},
	)
	if len(result) != 4 || result[0].Context != "CI/Test" || result[0].Required != true {
		t.Fatalf("decorated status checks = %#v", result)
	}
	if result[2].Context != "optional" || result[2].Required {
		t.Fatalf("optional status check was marked required: %#v", result[2])
	}
	if len(unsuccessful) != 3 || unsuccessful[0].State != "pending" ||
		unsuccessful[1].State != "missing" || unsuccessful[2].State != "error" {
		t.Fatalf("unsuccessful status checks = %#v", unsuccessful)
	}
	blocker, blocked := requiredStatusChecksBlocker(unsuccessful)
	if !blocked || blocker.Code != "required_status_checks" {
		t.Fatalf("required status blocker = %+v, blocked=%t", blocker, blocked)
	}
	for _, value := range []string{"lint (pending)", "missing (missing)", "security (error)"} {
		if !strings.Contains(blocker.Detail, value) {
			t.Errorf("blocker detail %q does not contain %q", blocker.Detail, value)
		}
	}
	if blocker, blocked := requiredStatusChecksBlocker(nil); blocked || blocker.Code != "" {
		t.Fatalf("empty status blocker = %+v, blocked=%t", blocker, blocked)
	}
}

func TestEvaluateRequiredStatusChecksRejectsFailure(t *testing.T) {
	_, unsuccessful := evaluateRequiredStatusChecks(
		[]collab.RevisionStatusCheck{{Context: "build", State: "failure"}},
		[]string{"build"},
	)
	if len(unsuccessful) != 1 || unsuccessful[0].State != "failure" {
		t.Fatalf("failure was not reported: %#v", unsuccessful)
	}
}
