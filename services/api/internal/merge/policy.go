package merge

import (
	"sort"
	"strings"

	"github.com/lorehub/lorehub/services/api/internal/collab"
)

// MatchBranchPattern uses a small deterministic glob matcher. '*' matches any
// sequence and '?' matches one rune; all other characters match literally.
func MatchBranchPattern(pattern, branch string) bool {
	patternRunes := []rune(pattern)
	branchRunes := []rune(branch)
	previous := make([]bool, len(branchRunes)+1)
	previous[0] = true
	for _, patternRune := range patternRunes {
		current := make([]bool, len(branchRunes)+1)
		if patternRune == '*' {
			current[0] = previous[0]
			for index := 1; index <= len(branchRunes); index++ {
				current[index] = previous[index] || current[index-1]
			}
		} else {
			for index := 1; index <= len(branchRunes); index++ {
				current[index] = previous[index-1] && (patternRune == '?' || patternRune == branchRunes[index-1])
			}
		}
		previous = current
	}
	return previous[len(branchRunes)]
}

func matchingRules(rules []collab.BranchRule, branch string) []collab.BranchRule {
	matched := make([]collab.BranchRule, 0, len(rules))
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

func requiredApprovals(rules []collab.BranchRule) int {
	required := 0
	for _, rule := range rules {
		if rule.RequiredApprovals > required {
			required = rule.RequiredApprovals
		}
	}
	return required
}

func requiresCI(rules []collab.BranchRule) bool {
	for _, rule := range rules {
		if rule.RequireCISuccess {
			return true
		}
	}
	return false
}

func directPushBlocked(rules []collab.BranchRule) bool {
	for _, rule := range rules {
		if rule.BlockDirectPush {
			return true
		}
	}
	return false
}
