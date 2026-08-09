package lore

import "testing"

func TestValidateMergeWorkspacePaths(t *testing.T) {
	for _, path := range []string{"conflict.txt", "src/main.go", "日本語/README.md"} {
		if err := validateMergeWorkspacePaths([]string{path}); err != nil {
			t.Errorf("valid merge path %q returned %v", path, err)
		}
	}
	for _, path := range []string{"", "/tmp/file", "../file", "src/../file", `src\\file`, "src/\x00file"} {
		if err := validateMergeWorkspacePaths([]string{path}); err == nil {
			t.Errorf("unsafe merge path %q was accepted", path)
		}
	}
}

func TestSameMergeParentsIsOrderIndependentButExact(t *testing.T) {
	source := "source"
	target := "target"
	if !sameMergeParents([]string{source, target}, source, target) {
		t.Fatal("source/target parents were rejected")
	}
	if !sameMergeParents([]string{target, source}, source, target) {
		t.Fatal("target/source parents were rejected")
	}
	for _, parents := range [][]string{{source}, {source, target, "extra"}, {"other", target}} {
		if sameMergeParents(parents, source, target) {
			t.Fatalf("invalid parents were accepted: %#v", parents)
		}
	}
}
