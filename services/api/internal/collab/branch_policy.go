package collab

import (
	"sort"
	"strings"

	"github.com/lorehub/lorehub/services/api/internal/authz"
)

// MatchBranchPattern is a deterministic full-match glob: '*' spans any
// sequence of runes, '?' spans one rune, and every other rune is literal.
func MatchBranchPattern(pattern, branch string) bool {
	return authz.MatchBranchPattern(pattern, branch)
}

func MatchingBranchRules(rules []BranchRule, branch string) []BranchRule {
	matched := make([]BranchRule, 0, len(rules))
	for _, rule := range rules {
		if MatchBranchPattern(rule.Pattern, branch) {
			matched = append(matched, rule)
		}
	}
	sort.SliceStable(matched, func(left, right int) bool {
		return strings.Compare(matched[left].Pattern, matched[right].Pattern) < 0
	})
	return matched
}

func RequiredBranchApprovals(rules []BranchRule) int {
	required := 0
	for _, rule := range rules {
		if rule.RequiredApprovals > required {
			required = rule.RequiredApprovals
		}
	}
	return required
}

func BranchRequiresCI(rules []BranchRule) bool {
	for _, rule := range rules {
		if rule.RequireCISuccess {
			return true
		}
	}
	return false
}

func BranchBlocksDirectPush(rules []BranchRule) bool {
	for _, rule := range rules {
		if rule.BlockDirectPush {
			return true
		}
	}
	return false
}
