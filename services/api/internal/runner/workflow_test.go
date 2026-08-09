package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdaptWorkflowsUsesLoreWorkspaceCheckout(t *testing.T) {
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
	if !strings.Contains(string(contents), "actions/checkout@v4") {
		t.Fatal("workflow no longer uses act's Lore workspace checkout path")
	}
	if strings.Contains(string(contents), ".lorehub/actions/checkout") {
		t.Fatal("workflow was rewritten to an unsupported local action")
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

func TestDiscoverWorkflowsRejectsContainerAndServiceOptions(t *testing.T) {
	tests := []struct {
		name       string
		definition string
	}{
		{
			name: "container options",
			definition: `    container:
      image: node:22
      options: --privileged
`,
		},
		{
			name: "service options",
			definition: `    services:
      database:
        image: postgres:18
        options: --network host
`,
		},
		{
			name: "container volumes",
			definition: `    container:
      image: node:22
      volumes: [/var/run/docker.sock:/var/run/docker.sock]
`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			workflowDirectory := filepath.Join(workspace, ".github", "workflows")
			if err := os.MkdirAll(workflowDirectory, 0o750); err != nil {
				t.Fatal(err)
			}
			contents := `name: Unsafe
on: push
jobs:
  test:
    runs-on: ubuntu-latest
` + test.definition + `    steps:
      - run: echo ok
`
			path := filepath.Join(workflowDirectory, "unsafe.yml")
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			workflows, err := DiscoverWorkflows(workspace)
			if err != nil {
				t.Fatal(err)
			}
			if len(workflows) != 1 || workflows[0].Enabled || workflows[0].State != "error" ||
				workflows[0].ErrorCode != "unsupported_runtime_definition" {
				t.Fatalf("unsafe runtime definition was not disabled: %#v", workflows)
			}
			if !strings.Contains(workflows[0].ErrorMessage, "unsupported") {
				t.Fatalf("unsafe runtime definition has no clear error: %q", workflows[0].ErrorMessage)
			}
		})
	}
}

func TestDiscoverWorkflowsAcceptsEmptyRuntimeOptions(t *testing.T) {
	workspace := t.TempDir()
	workflowDirectory := filepath.Join(workspace, ".github", "workflows")
	if err := os.MkdirAll(workflowDirectory, 0o750); err != nil {
		t.Fatal(err)
	}
	contents := `name: Safe
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    container:
      image: node:22
      options: ""
    services:
      database:
        image: postgres:18
        options: ""
    steps:
      - run: echo ok
`
	if err := os.WriteFile(filepath.Join(workflowDirectory, "safe.yml"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	workflows, err := DiscoverWorkflows(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(workflows) != 1 || !workflows[0].Enabled || workflows[0].State != "active" {
		t.Fatalf("empty runtime options were not accepted: %#v", workflows)
	}
}

func TestDiscoverWorkflowsRejectsSymlinkedWorkflowDirectory(t *testing.T) {
	workspace := t.TempDir()
	target := filepath.Join(t.TempDir(), "workflows")
	if err := os.MkdirAll(target, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(workspace, ".github"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(workspace, ".github", "workflows")); err != nil {
		t.Fatal(err)
	}
	if _, err := DiscoverWorkflows(workspace); err == nil {
		t.Fatal("symlinked workflow directory was inspected")
	}
}
