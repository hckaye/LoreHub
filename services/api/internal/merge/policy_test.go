package merge

import (
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/collab"
)

func TestMatchBranchPattern(t *testing.T) {
	tests := []struct {
		pattern string
		branch  string
		want    bool
	}{
		{pattern: "main", branch: "main", want: true},
		{pattern: "main", branch: "mainline", want: false},
		{pattern: "release/*", branch: "release/2026.08", want: true},
		{pattern: "release/*", branch: "release/a/b", want: true},
		{pattern: "hotfix/?", branch: "hotfix/a", want: true},
		{pattern: "hotfix/?", branch: "hotfix/ab", want: false},
		{pattern: "*", branch: "", want: true},
	}
	for _, test := range tests {
		if got := MatchBranchPattern(test.pattern, test.branch); got != test.want {
			t.Errorf("MatchBranchPattern(%q, %q) = %v, want %v", test.pattern, test.branch, got, test.want)
		}
	}
}

func TestMatchingRulesAreDeterministicAndMostRestrictive(t *testing.T) {
	rules := []collab.BranchRule{
		{Pattern: "release/*", RequiredApprovals: 1, RequireCISuccess: false, BlockDirectPush: false},
		{Pattern: "*", RequiredApprovals: 2, RequireCISuccess: true, BlockDirectPush: true},
		{Pattern: "release/2026.*", RequiredApprovals: 3, RequireCISuccess: false, BlockDirectPush: false},
	}
	matched := matchingRules(rules, "release/2026.08")
	if len(matched) != 3 || matched[0].Pattern != "*" || matched[1].Pattern != "release/*" ||
		matched[2].Pattern != "release/2026.*" {
		t.Fatalf("matching rules = %#v, want deterministic pattern order", matched)
	}
	if got := requiredApprovals(matched); got != 3 {
		t.Fatalf("required approvals = %d, want 3", got)
	}
	if !requiresCI(matched) || !directPushBlocked(matched) {
		t.Fatalf("combined branch rule policy = ci=%v direct-push=%v, want both enabled",
			requiresCI(matched), directPushBlocked(matched))
	}
}

func TestMatchingRulesDoNotUseSubstringSemantics(t *testing.T) {
	rules := []collab.BranchRule{{Pattern: "main"}, {Pattern: "release/*"}}
	if got := matchingRules(rules, "feature/main"); len(got) != 0 {
		t.Fatalf("substring branch match returned %#v", got)
	}
	if got := matchingRules(rules, "release"); len(got) != 0 {
		t.Fatalf("partial wildcard branch match returned %#v", got)
	}
}

func TestRestartIgnoresRequirementsThatMustBeReevaluatedAfterRevisionRefresh(t *testing.T) {
	blockers := []collab.MergeBlocker{
		{Code: "stale_source_revision"},
		{Code: "stale_target_revision"},
		{Code: "required_approvals"},
		{Code: "changes_requested"},
		{Code: "ci_required"},
	}
	if hasRestartBlocker(blockers) {
		t.Fatal("revision-dependent policy blockers should not prevent restarting a stale operation")
	}
	if !hasRestartBlocker([]collab.MergeBlocker{{Code: "write_permission_required"}}) {
		t.Fatal("restart should still require write permission")
	}
}
