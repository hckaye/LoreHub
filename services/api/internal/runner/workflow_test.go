package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdaptWorkflowsReplacesCheckoutAction(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	workflowDirectory := filepath.Join(workspace, ".github", "workflows")
	if err := os.MkdirAll(workflowDirectory, 0o750); err != nil {
		t.Fatal(err)
	}
	workflow := `name: CI
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: npm test
`
	path := filepath.Join(workflowDirectory, "ci.yml")
	if err := os.WriteFile(path, []byte(workflow), 0o600); err != nil {
		t.Fatal(err)
	}
	replacements, err := AdaptWorkflows(workspace)
	if err != nil {
		t.Fatalf("AdaptWorkflows returned an error: %v", err)
	}
	if replacements != 1 {
		t.Fatalf("expected one replacement, got %d", replacements)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "./.lorehub/actions/checkout") {
		t.Fatal("workflow does not reference the Lore checkout adapter")
	}
}
