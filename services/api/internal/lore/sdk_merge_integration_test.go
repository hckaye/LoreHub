package lore

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSDKMergeLifecycleAgainstLoreServer(t *testing.T) {
	if os.Getenv("LOREHUB_TEST_LORE_MERGE") != "1" {
		t.Skip("LOREHUB_TEST_LORE_MERGE is not set to 1")
	}
	repositoryURL := os.Getenv("LOREHUB_TEST_LORE_URL")
	cliPath := os.Getenv("LOREHUB_TEST_LORE_CLI")
	worktree := os.Getenv("LOREHUB_TEST_LORE_WORKTREE")
	if repositoryURL == "" || cliPath == "" || worktree == "" {
		t.Fatal("LOREHUB_TEST_LORE_URL, LOREHUB_TEST_LORE_CLI, and LOREHUB_TEST_LORE_WORKTREE are required")
	}
	identity := os.Getenv("LOREHUB_TEST_LORE_IDENTITY")
	if identity == "" {
		identity = "fixture"
	}
	client, err := NewSDKClient(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	repository, err := client.RepositoryInfo(ctx, repositoryURL, identity)
	if err != nil {
		t.Fatalf("RepositoryInfo returned an error: %v", err)
	}
	ref := RepositoryRef{
		CacheKey:         "merge-integration",
		URL:              repositoryURL,
		LoreRepositoryID: repository.ID,
		DefaultBranch:    repository.DefaultBranch,
	}
	merge := MergeClient(client)

	cleanSource, err := fixtureBranchRevision(ctx, client, ref, "feature-clean", identity)
	if err != nil {
		t.Fatal(err)
	}
	cleanTarget, err := fixtureBranchRevision(ctx, client, ref, "main", identity)
	if err != nil {
		t.Fatal(err)
	}
	clean, err := merge.StartMerge(ctx, ref, "merge-lifecycle-clean", "feature-clean", "main",
		cleanSource, cleanTarget, "Merge clean feature", identity)
	if err != nil {
		t.Fatalf("clean StartMerge returned an error: %v", err)
	}
	if len(clean.Conflicts) != 0 || clean.StagedRevision == "" {
		t.Fatalf("clean merge did not stage a revision: %#v", clean)
	}
	cleanPush, err := merge.PushMerge(ctx, ref, "merge-lifecycle-clean", "main", identity)
	if err != nil {
		t.Fatalf("clean PushMerge returned an error: %v", err)
	}
	if cleanPush.RemoteRevision == "" {
		t.Fatalf("clean push returned no remote revision: %#v", cleanPush)
	}
	if err := merge.CleanupMergeWorkspace(ctx, ref, "merge-lifecycle-clean"); err != nil {
		t.Fatalf("clean workspace cleanup returned an error: %v", err)
	}

	conflictSource, err := fixtureBranchRevision(ctx, client, ref, "feature-conflict", identity)
	if err != nil {
		t.Fatal(err)
	}
	conflictTarget, err := fixtureBranchRevision(ctx, client, ref, "conflict-target", identity)
	if err != nil {
		t.Fatal(err)
	}
	abortOperation := "merge-lifecycle-abort"
	abortStart, err := merge.StartMerge(ctx, ref, abortOperation, "feature-conflict", "conflict-target",
		conflictSource, conflictTarget, "Conflict abort", identity)
	if err != nil {
		t.Fatalf("conflict StartMerge returned an error: %v", err)
	}
	if len(abortStart.Conflicts) == 0 {
		t.Fatalf("conflict merge did not report conflicts: %#v", abortStart)
	}
	listed, err := merge.ListConflicts(ctx, ref, abortOperation, nil, identity)
	if err != nil || len(listed) == 0 {
		t.Fatalf("ListConflicts returned %v and %#v", err, listed)
	}
	if err := merge.AbortMerge(ctx, ref, abortOperation, identity); err != nil {
		t.Fatalf("AbortMerge returned an error: %v", err)
	}
	if err := merge.CleanupMergeWorkspace(ctx, ref, abortOperation); err != nil {
		t.Fatalf("aborted workspace cleanup returned an error: %v", err)
	}

	restartSource, err := fixtureBranchRevision(ctx, client, ref, "feature-conflict", identity)
	if err != nil {
		t.Fatal(err)
	}
	restartTarget, err := fixtureBranchRevision(ctx, client, ref, "conflict-target", identity)
	if err != nil {
		t.Fatal(err)
	}
	restartOperation := "merge-lifecycle-restart"
	restartStart, err := merge.StartMerge(ctx, ref, restartOperation, "feature-conflict", "conflict-target",
		restartSource, restartTarget, "Conflict restart", identity)
	if err != nil {
		t.Fatalf("restart StartMerge returned an error: %v", err)
	}
	if len(restartStart.Conflicts) == 0 {
		t.Fatalf("restart merge did not report initial conflicts: %#v", restartStart)
	}
	advanceFixtureTarget(t, ctx, cliPath, worktree, identity)
	newTarget, err := fixtureBranchRevision(ctx, client, ref, "conflict-target", identity)
	if err != nil {
		t.Fatal(err)
	}
	if newTarget == restartTarget {
		t.Fatalf("fixture target revision did not change: %s", newTarget)
	}
	restartedConflicts, err := merge.RestartMerge(ctx, ref, restartOperation, "feature-conflict", "conflict-target",
		restartSource, newTarget, nil, identity)
	if err != nil {
		t.Fatalf("RestartMerge returned an error: %v", err)
	}
	if len(restartedConflicts) == 0 {
		t.Fatalf("RestartMerge lost the expected conflict: %#v", restartedConflicts)
	}
	if _, err := merge.ResolveMerge(ctx, ref, restartOperation, restartedConflicts, "theirs", identity); err != nil {
		t.Fatalf("ResolveMerge returned an error: %v", err)
	}
	remaining, err := merge.ListConflicts(ctx, ref, restartOperation, nil, identity)
	if err != nil {
		t.Fatalf("ListConflicts after resolution returned an error: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("resolved merge still has conflicts: %#v", remaining)
	}
	restartedPush, err := merge.PushMerge(ctx, ref, restartOperation, "conflict-target", identity)
	if err != nil {
		t.Fatalf("restarted PushMerge returned an error: %v", err)
	}
	if restartedPush.RemoteRevision == "" {
		t.Fatalf("restarted push returned no remote revision: %#v", restartedPush)
	}
	if err := merge.CleanupMergeWorkspace(ctx, ref, restartOperation); err != nil {
		t.Fatalf("restarted workspace cleanup returned an error: %v", err)
	}
}

func fixtureBranchRevision(
	ctx context.Context,
	client *SDKClient,
	ref RepositoryRef,
	branch string,
	identity string,
) (string, error) {
	branches, err := client.Branches(ctx, ref, identity)
	if err != nil {
		return "", fmt.Errorf("list fixture branches: %w", err)
	}
	for _, item := range branches {
		if item.Name == branch && item.LatestRevision != "" {
			return item.LatestRevision, nil
		}
	}
	return "", fmt.Errorf("fixture branch %q was not found: %#v", branch, branches)
}

func advanceFixtureTarget(t *testing.T, ctx context.Context, cliPath, worktree, identity string) {
	t.Helper()
	args := []string{"--repository", worktree, "--identity", identity, "branch", "switch", "conflict-target", "--reset"}
	runFixtureCLI(t, ctx, cliPath, args...)
	if err := os.WriteFile(filepath.Join(worktree, "restart-target.txt"), []byte("target changed\n"), 0o644); err != nil {
		t.Fatalf("write target fixture change: %v", err)
	}
	runFixtureCLI(t, ctx, cliPath, "--repository", worktree, "stage", "--scan", ".")
	runFixtureCLI(t, ctx, cliPath, "--repository", worktree, "--identity", identity, "commit", "advance target")
	runFixtureCLI(t, ctx, cliPath, "--repository", worktree, "--identity", identity, "push", "conflict-target")
}

func runFixtureCLI(t *testing.T, ctx context.Context, cliPath string, args ...string) {
	t.Helper()
	command := exec.CommandContext(ctx, cliPath, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Lore fixture command %q failed: %v\n%s", strings.Join(args, " "), err, output)
	}
}
