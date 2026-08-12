package merge

import (
	"github.com/lorehub/lorehub/services/api/internal/collab"
)

// MatchBranchPattern uses a small deterministic glob matcher. '*' matches any
// sequence and '?' matches one rune; all other characters match literally.
func MatchBranchPattern(pattern, branch string) bool {
	return collab.MatchBranchPattern(pattern, branch)
}

func matchingRules(rules []collab.BranchRule, branch string) []collab.BranchRule {
	return collab.MatchingBranchRules(rules, branch)
}

func requiredApprovals(rules []collab.BranchRule) int {
	return collab.RequiredBranchApprovals(rules)
}

func requiresCI(rules []collab.BranchRule) bool {
	return collab.BranchRequiresCI(rules)
}

func requiredStatusChecks(rules []collab.BranchRule) []string {
	return collab.RequiredBranchStatusChecks(rules)
}

func directPushBlocked(rules []collab.BranchRule) bool {
	return collab.BranchBlocksDirectPush(rules)
}
