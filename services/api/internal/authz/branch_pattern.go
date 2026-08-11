package authz

// MatchBranchPattern is a deterministic full-match glob. An asterisk spans
// any sequence of runes, a question mark spans one rune, and all other runes
// are literal.
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
