package collab

import (
	"sort"
	"strings"
)

// MatchBranchPattern is a deterministic full-match glob: '*' spans any
// sequence of runes, '?' spans one rune, and every other rune is literal.
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
				current[index] = previous[index-1] &&
					(patternRune == '?' || patternRune == branchRunes[index-1])
			}
		}
		previous = current
	}
	return previous[len(branchRunes)]
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
