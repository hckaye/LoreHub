package code

import "testing"

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		name  string
		value string
		valid bool
	}{
		{name: "root", value: "", valid: true},
		{name: "nested file", value: "src/main.go", valid: true},
		{name: "absolute", value: "/src/main.go"},
		{name: "parent", value: "src/../main.go"},
		{name: "current", value: "src/./main.go"},
		{name: "double slash", value: "src//main.go"},
		{name: "trailing slash", value: "src/"},
		{name: "backslash", value: `src\\main.go`},
		{name: "nul", value: "src/\x00main.go"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizePath(test.value)
			if test.valid && err != nil {
				t.Fatalf("normalizePath(%q) returned %v", test.value, err)
			}
			if !test.valid && err == nil {
				t.Fatalf("normalizePath(%q) accepted an unsafe path", test.value)
			}
		})
	}
}

func TestNormalizeRevision(t *testing.T) {
	valid := "ABCDEF0123456789abcdef0123456789ABCDEF0123456789abcdef0123456789"
	got, err := normalizeRevision(valid)
	if err != nil {
		t.Fatalf("normalizeRevision returned %v", err)
	}
	if got != "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789" {
		t.Fatalf("normalized revision = %q", got)
	}
	for _, value := range []string{"", "abc", valid[:63], valid + "0", "not-a-revision"} {
		if _, err := normalizeRevision(value); err == nil {
			t.Errorf("normalizeRevision(%q) accepted an invalid revision", value)
		}
	}
}

func TestNormalizeBranch(t *testing.T) {
	for _, value := range []string{"main", "release/2026.08", "feature/日本語"} {
		if _, err := normalizeBranch(value); err != nil {
			t.Errorf("normalizeBranch(%q) returned %v", value, err)
		}
	}
	for _, value := range []string{"", "/main", "main/", "release//x", "release/../main", `feature\\x`} {
		if _, err := normalizeBranch(value); err == nil {
			t.Errorf("normalizeBranch(%q) accepted an invalid branch", value)
		}
	}
}
