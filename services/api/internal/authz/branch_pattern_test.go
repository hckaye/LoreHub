package authz

import "testing"

func TestMatchBranchPattern(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		pattern string
		branch  string
		want    bool
	}{
		{name: "exact", pattern: "main", branch: "main", want: true},
		{name: "asterisk crosses slash", pattern: "feature/*", branch: "feature/ui/mobile", want: true},
		{name: "question mark", pattern: "release/?", branch: "release/1", want: true},
		{name: "question mark is one rune", pattern: "release/?", branch: "release/10", want: false},
		{name: "literal regex punctuation", pattern: "release/[1]", branch: "release/1", want: false},
		{name: "full match", pattern: "main", branch: "main-old", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := MatchBranchPattern(test.pattern, test.branch); got != test.want {
				t.Fatalf("MatchBranchPattern(%q, %q) = %t, want %t", test.pattern, test.branch, got, test.want)
			}
		})
	}
}
