package reviewthreads

import (
	"errors"
	"testing"

	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
)

func TestLineFromDiffUsesUnifiedDiffSides(t *testing.T) {
	diff := loreclient.Diff{
		Source: "base", Target: "head",
		Files: []loreclient.DiffFile{{
			Path:  "src/main.go",
			Patch: "@@ -10,3 +10,4 @@\n context\n-old value\n+new value\n+extra\n tail\n",
		}},
	}
	tests := []struct {
		name string
		side Side
		line int
		want string
	}{
		{name: "left context", side: SideLeft, line: 10, want: "context"},
		{name: "right context", side: SideRight, line: 10, want: "context"},
		{name: "removed", side: SideLeft, line: 11, want: "old value"},
		{name: "added", side: SideRight, line: 11, want: "new value"},
		{name: "second added", side: SideRight, line: 12, want: "extra"},
		{name: "left tail", side: SideLeft, line: 12, want: "tail"},
		{name: "right tail", side: SideRight, line: 13, want: "tail"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := lineFromDiff(diff, "src/main.go", test.side, test.line)
			if err != nil || got != test.want {
				t.Fatalf("line = %q, error = %v, want %q", got, err, test.want)
			}
		})
	}
	if _, err := lineFromDiff(diff, "src/main.go", SideRight, 99); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing line error = %v, want invalid input", err)
	}
}

func TestLineFromDiffRejectsIncompleteFiles(t *testing.T) {
	for _, file := range []loreclient.DiffFile{
		{Path: "asset.bin", Binary: true},
		{Path: "large.txt", Patch: "@@ -1 +1 @@\n-a\n+b", Truncated: true},
		{Path: "empty.txt"},
	} {
		_, err := lineFromDiff(loreclient.Diff{Files: []loreclient.DiffFile{file}}, file.Path, SideRight, 1)
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("file %+v error = %v, want invalid input", file, err)
		}
	}
}
