package merge

import (
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/collab"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
)

func TestSelectRecoveryRevisionUsesOnlyExactRemoteStates(t *testing.T) {
	operation := collab.MergeOperation{
		SourceRevision: "source-old", TargetRevision: "target-old",
		StagedRevision: "staged-merge", PushedRevision: "pushed-merge",
	}
	tests := map[string]struct {
		source string
		target string
		want   string
	}{
		"staged remote":     {source: "source-new", target: "staged-merge", want: "staged-merge"},
		"pushed remote":     {source: "source-new", target: "pushed-merge", want: "pushed-merge"},
		"exact old target":  {source: "source-old", target: "target-old", want: ""},
		"unexpected target": {source: "source-old", target: "target-new", want: "staged-merge"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			branches := []loreclient.Branch{
				{Name: "feature", LatestRevision: test.source},
				{Name: "main", LatestRevision: test.target},
			}
			if got := selectRecoveryRevision(operation, "feature", "main", branches); got != test.want {
				t.Fatalf("recovery revision = %q, want %q", got, test.want)
			}
		})
	}
}
