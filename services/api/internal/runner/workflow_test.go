package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	ordered := WorkflowDefinition{
		Enabled: true,
		State:   "active",
		Push:    &PushTrigger{Branches: []string{"release/**", "!release/paused", "release/reopened"}},
	}
	if !ordered.MatchesPush("release/v1") || ordered.MatchesPush("release/paused") ||
		!ordered.MatchesPush("release/reopened") {
		t.Fatalf("ordered branch negation did not match as expected: %#v", ordered.Push)
	}
}

func TestDiscoverWorkflowsAcceptsAllSupportedEvents(t *testing.T) {
	workspace := t.TempDir()
	workflowDirectory := filepath.Join(workspace, ".github", "workflows")
	if err := os.MkdirAll(workflowDirectory, 0o750); err != nil {
		t.Fatal(err)
	}
	contents := `name: Supported events
on:
  push:
  workflow_dispatch:
  pull_request:
    branches: [main]
    types: [opened, synchronize]
  schedule:
    - cron: "*/15 * * * *"
  repository_dispatch:
    types: [refresh]
jobs:
  test:
    runs-on: ubuntu-latest
`
	if err := os.WriteFile(filepath.Join(workflowDirectory, "supported.yaml"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	workflows, err := DiscoverWorkflows(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(workflows) != 1 || !workflows[0].Enabled || workflows[0].State != "active" ||
		workflows[0].PullRequest == nil || len(workflows[0].Schedules) != 1 ||
		workflows[0].RepositoryDispatch == nil {
		t.Fatalf("supported events were not recorded: %#v", workflows)
	}
	if !workflows[0].MatchesPullRequest("main", "opened") ||
		workflows[0].MatchesPullRequest("feature", "opened") ||
		!workflows[0].MatchesRepositoryDispatch("refresh") ||
		workflows[0].MatchesRepositoryDispatch("other") {
		t.Fatalf("supported event filters did not match: %#v", workflows[0])
	}
	if occurrence, ok := LastScheduleOccurrence("*/15 * * * *",
		time.Date(2026, time.August, 9, 12, 31, 0, 0, time.UTC)); !ok || occurrence.Minute() != 30 {
		t.Fatalf("schedule time was not calculated in UTC: %s, %t", occurrence, ok)
	}
}

func TestWorkflowDispatchInputsArePreservedAndValidated(t *testing.T) {
	workspace := t.TempDir()
	workflowDirectory := filepath.Join(workspace, ".github", "workflows")
	if err := os.MkdirAll(workflowDirectory, 0o750); err != nil {
		t.Fatal(err)
	}
	contents := `name: Manual
on:
  workflow_dispatch:
    inputs:
      target:
        description: Release channel
        required: true
        type: choice
        options: [stable, beta]
      dry_run:
        description: Preview only
        type: boolean
        default: false
jobs:
  release:
    runs-on: ubuntu-latest
`
	if err := os.WriteFile(filepath.Join(workflowDirectory, "manual.yml"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	workflows, err := DiscoverWorkflows(workspace)
	if err != nil || len(workflows) != 1 {
		t.Fatalf("workflow input discovery failed: %#v, %v", workflows, err)
	}
	definition := workflows[0]
	target, ok := definition.DispatchInputs["target"]
	if !ok || target.Description != "Release channel" || !target.Required || target.Type != "choice" ||
		len(target.Options) != 2 {
		t.Fatalf("workflow dispatch definition was not preserved: %#v", definition.DispatchInputs)
	}
	dryRun, ok := definition.DispatchInputs["dry_run"]
	if !ok || dryRun.Default == nil || *dryRun.Default != "false" {
		t.Fatalf("boolean default was not preserved: %#v", definition.DispatchInputs)
	}
	if !json.Valid(definition.TriggerConfig) {
		t.Fatalf("trigger configuration is not valid JSON: %s", definition.TriggerConfig)
	}
	resolved, err := ResolveWorkflowDispatchInputs(definition, map[string]string{"target": "beta"})
	if err != nil || resolved["target"] != "beta" || resolved["dry_run"] != "false" {
		t.Fatalf("workflow dispatch inputs were not resolved: %#v, %v", resolved, err)
	}
	if _, err := ResolveWorkflowDispatchInputs(definition, map[string]string{"target": "nightly"}); err == nil {
		t.Fatal("workflow dispatch accepted an option outside the configured choices")
	}
	if _, err := ResolveWorkflowDispatchInputs(definition, nil); err == nil {
		t.Fatal("workflow dispatch omitted a required input")
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
      image: alpine:3.24.1
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
      image: alpine:3.24.1
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
      image: alpine:3.24.1
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
