package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunnerPlatformMappingDefaultsAndFailsClosed(t *testing.T) {
	if DefaultRunnerPlatformImages()["ubuntu-latest"] != DefaultUbuntuLatestImage {
		t.Fatal("ubuntu-latest does not use the pinned act Ubuntu image")
	}
	merged, err := mergedRunnerPlatformImages(map[string]string{
		"self-hosted-linux": "ghcr.io/example/runner:1.2.3",
	})
	if err != nil || merged["ubuntu-latest"] != DefaultUbuntuLatestImage {
		t.Fatalf("custom platform mapping removed the default: %#v, %v", merged, err)
	}
	workspace := t.TempDir()
	directory := filepath.Join(workspace, ".github", "workflows")
	if err := os.MkdirAll(directory, 0o750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "custom.yml")
	workflow := "name: Custom\non: push\njobs:\n  test:\n    runs-on: self-hosted-linux\n"
	if err := os.WriteFile(path, []byte(workflow), 0o600); err != nil {
		t.Fatal(err)
	}
	workflows, err := DiscoverWorkflows(workspace)
	if err != nil || len(workflows) != 1 || workflows[0].Enabled ||
		!strings.Contains(workflows[0].ErrorMessage, "not mapped") {
		t.Fatalf("unmapped runner label was not disabled: %#v, %v", workflows, err)
	}
	workflows, err = DiscoverWorkflows(workspace, map[string]string{
		"self-hosted-linux": "ghcr.io/example/runner:1.2.3",
	})
	if err != nil || len(workflows) != 1 || !workflows[0].Enabled || workflows[0].State != "active" {
		t.Fatalf("configured runner label was not accepted: %#v, %v", workflows, err)
	}
}

func TestRunnerPlatformMappingRejectsWorkflowImageInjection(t *testing.T) {
	if err := ValidateRunnerPlatformImages(map[string]string{
		"ubuntu-latest": "ghcr.io/example/runner:1.2.3 --privileged",
	}); err == nil {
		t.Fatal("runner image with command injection was accepted")
	}
	if err := ValidateRunnerPlatformImages(map[string]string{
		"ubuntu-latest": "ghcr.io/example/runner@sha256:abc",
	}); err == nil {
		t.Fatal("digest image was accepted despite exact tag deployment policy")
	}
}

func TestDiscoverRunnerLabelsNormalizesAndRejectsMixedJobs(t *testing.T) {
	workspace := t.TempDir()
	directory := filepath.Join(workspace, ".github", "workflows")
	if err := os.MkdirAll(directory, 0o750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "self-hosted.yml")
	workflow := `name: Self-hosted
on: push
jobs:
  build:
    runs-on: [SELF-HOSTED, Linux, X64]
  test:
    runs-on: [x64, self-hosted, linux]
`
	if err := os.WriteFile(path, []byte(workflow), 0o600); err != nil {
		t.Fatal(err)
	}
	workflows, err := DiscoverWorkflows(workspace)
	if err != nil || len(workflows) != 1 || !workflows[0].Enabled {
		t.Fatalf("normalized self-hosted workflow was rejected: %#v, %v", workflows, err)
	}
	expected := []string{"linux", "self-hosted", "x64"}
	if !equalRunnerLabels(workflows[0].RunnerLabels, expected) {
		t.Fatalf("runner labels were not normalized: %#v", workflows[0].RunnerLabels)
	}
	restored, err := workflowFromTriggerConfig(
		workflows[0].Path, workflows[0].Name, true, "active", workflows[0].TriggerConfig,
	)
	if err != nil || !equalRunnerLabels(restored.RunnerLabels, expected) {
		t.Fatalf("runner labels were not persisted: %#v, %v", restored.RunnerLabels, err)
	}

	mixed := strings.Replace(workflow, "runs-on: [x64, self-hosted, linux]", "runs-on: ubuntu-latest", 1)
	if err := os.WriteFile(path, []byte(mixed), 0o600); err != nil {
		t.Fatal(err)
	}
	workflows, err = DiscoverWorkflows(workspace)
	if err != nil || len(workflows) != 1 || workflows[0].Enabled ||
		!strings.Contains(workflows[0].ErrorMessage, "all jobs in a workflow must use the same") {
		t.Fatalf("mixed runs-on workflow was not rejected actionably: %#v, %v", workflows, err)
	}
}
