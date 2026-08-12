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

// RequiredBranchStatusChecks returns the sorted union of status contexts from
// every matching rule.
func RequiredBranchStatusChecks(rules []BranchRule) []string {
	unique := make(map[string]string)
	for _, rule := range rules {
		for _, contextName := range rule.RequiredStatusChecks {
			key := strings.ToLower(contextName)
			if _, found := unique[key]; !found {
				unique[key] = contextName
			}
		}
	}
	checks := make([]string, 0, len(unique))
	for _, contextName := range unique {
		checks = append(checks, contextName)
	}
	sort.SliceStable(checks, func(left, right int) bool {
		return strings.ToLower(checks[left]) < strings.ToLower(checks[right])
	})
	return checks
}

func RequiredBranchStatusChecksSuccessful(
	required []string,
	checks []RevisionStatusCheck,
) bool {
	states := make(map[string]string, len(checks))
	for _, check := range checks {
		states[strings.ToLower(check.Context)] = check.State
	}
	for _, contextName := range required {
		if states[strings.ToLower(contextName)] != "success" {
			return false
		}
	}
	return true
}

func BranchBlocksDirectPush(rules []BranchRule) bool {
	for _, rule := range rules {
		if rule.BlockDirectPush {
			return true
		}
	}
	return false
}
