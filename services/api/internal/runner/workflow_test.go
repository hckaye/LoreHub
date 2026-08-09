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

func TestDiscoverWorkflowsValidatesTriggersAndBranchFilters(t *testing.T) {
	workspace := t.TempDir()
	workflowDirectory := filepath.Join(workspace, ".github", "workflows")
	if err := os.MkdirAll(workflowDirectory, 0o750); err != nil {
		t.Fatal(err)
	}
	contents := `name: Checks
on:
  push:
    branches:
      - main
      - release/**
  workflow_dispatch: {}
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`
	if err := os.WriteFile(filepath.Join(workflowDirectory, "checks.yml"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	workflows, err := DiscoverWorkflows(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(workflows) != 1 || !workflows[0].Enabled || workflows[0].Name != "Checks" {
		t.Fatalf("unexpected workflow discovery: %#v", workflows)
	}
	if !workflows[0].MatchesPush("main") || !workflows[0].MatchesPush("release/v1") ||
		workflows[0].MatchesPush("feature/new") {
		t.Fatalf("branch filters did not match as expected: %#v", workflows[0].Push)
	}
	ignored := WorkflowDefinition{
		Enabled: true, State: "active", Push: &PushTrigger{BranchesIgnore: []string{"release/paused"}},
	}
	if !ignored.MatchesPush("main") || ignored.MatchesPush("release/paused") {
		t.Fatalf("branches-ignore did not match as expected: %#v", ignored.Push)
	}
}

func TestDiscoverWorkflowsRecordsUnsupportedEvent(t *testing.T) {
	workspace := t.TempDir()
	workflowDirectory := filepath.Join(workspace, ".github", "workflows")
	if err := os.MkdirAll(workflowDirectory, 0o750); err != nil {
		t.Fatal(err)
	}
	contents := "name: Unsupported\non: pull_request\njobs:\n  test:\n    runs-on: ubuntu-latest\n"
	if err := os.WriteFile(filepath.Join(workflowDirectory, "unsupported.yaml"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	workflows, err := DiscoverWorkflows(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(workflows) != 1 || workflows[0].State != "error" || workflows[0].Enabled ||
		workflows[0].ErrorCode != "unsupported_trigger" {
		t.Fatalf("unsupported event was not recorded: %#v", workflows)
	}
}

func TestDiscoverWorkflowsRejectsUnsupportedPushFilterCombination(t *testing.T) {
	workspace := t.TempDir()
	workflowDirectory := filepath.Join(workspace, ".github", "workflows")
	if err := os.MkdirAll(workflowDirectory, 0o750); err != nil {
		t.Fatal(err)
	}
	contents := `name: Invalid
on:
  push:
    branches: [main]
    branches-ignore: [release/paused]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo invalid
`
	if err := os.WriteFile(filepath.Join(workflowDirectory, "invalid.yml"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	workflows, err := DiscoverWorkflows(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(workflows) != 1 || workflows[0].State != "error" || workflows[0].Enabled {
		t.Fatalf("unsupported filter combination was not disabled: %#v", workflows)
	}
}
