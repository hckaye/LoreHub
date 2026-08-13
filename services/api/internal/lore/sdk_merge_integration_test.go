package lore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSDKMergeLifecycleAgainstLoreServer(t *testing.T) {
	// This fixture is unauthenticated component coverage, not production auth or hook evidence.
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
	bootstrapCredential := Credential{
		Partition: repositoryURLPartition(repositoryURL), Identity: identity, Scope: ScopeRead,
		Principal:           ServicePrincipal(ServicePurposeRepositoryRegistration, "fixture-registration"),
		InsecureDevelopment: true,
	}
	cacheDirectory := loreTestTempDir(t)
	client, err := NewDevelopmentSDKClient(cacheDirectory)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	repository, err := client.RepositoryInfo(ctx, repositoryURL, bootstrapCredential)
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
	readCredential := developmentCredential(repository.ID, identity, ScopeRead)
	writeCredential := developmentCredential(repository.ID, identity, ScopeWrite)

	cleanSource, err := fixtureBranchRevision(ctx, client, ref, "feature-clean", identity)
	if err != nil {
		t.Fatal(err)
	}
	cleanTarget, err := fixtureBranchRevision(ctx, client, ref, "main", identity)
	if err != nil {
		t.Fatal(err)
	}
	clean, err := merge.StartMerge(ctx, ref, "merge-lifecycle-clean", "feature-clean", "main",
		cleanSource, cleanTarget, "Merge clean feature", writeCredential)
	if err != nil {
		t.Fatalf("clean StartMerge returned an error: %v", err)
	}
	if len(clean.Conflicts) != 0 || clean.StagedRevision == "" {
		t.Fatalf("clean merge did not stage a revision: %#v", clean)
	}
	var authorizationCalls atomic.Int32
	authorizer := PushAuthorizerFunc(func(_ context.Context, input PushAuthorization) error {
		authorizationCalls.Add(1)
		if input.RepositoryPartition != repository.ID || input.OperationID == "" || input.TargetBranchID == "" ||
			input.TargetBranchName == "" || input.ExpectedTargetRevision == "" || input.ProposedRevision == "" ||
			input.SourceRevision == "" || !sameMergeParents(input.ParentRevisions, input.SourceRevision,
			input.ExpectedTargetRevision) {
			return fmt.Errorf("unexpected push authorization tuple: %+v", input)
		}
		if input.OperationID == "merge-lifecycle-clean" &&
			(input.ActorUserID != "" || input.RepositoryID != "" || input.TargetBranchName != "main" ||
				input.ExpectedTargetRevision != cleanTarget || input.SourceRevision != cleanSource ||
				input.ProposedRevision != clean.StagedRevision) {
			return fmt.Errorf("unexpected clean push authorization tuple: %+v", input)
		}
		return nil
	})
	if _, err := merge.PushMerge(ctx, ref, "merge-lifecycle-clean", MergeWorkspace{
		SourceBranch: "feature-clean", TargetBranch: "main", SourceRevision: cleanSource,
		TargetRevision: cleanTarget, Message: "Merge clean feature",
	}, clean.StagedRevision, readCredential, writeCredential,
		PushAuthorizerFunc(func(context.Context, PushAuthorization) error {
			return fmt.Errorf("%w: deliberately denied", ErrPushAuthorizationDenied)
		})); !errors.Is(err, ErrPushAuthorizationDenied) {
		t.Fatalf("denied clean push error = %v, want ErrPushAuthorizationDenied", err)
	}
	unchangedTarget, err := fixtureBranchRevision(ctx, client, ref, "main", identity)
	if err != nil {
		t.Fatal(err)
	}
	if unchangedTarget != cleanTarget {
		t.Fatalf("Lore remote changed after authorization denial: %s != %s", unchangedTarget, cleanTarget)
	}
	assertMergeParents(t, clean.Parents, cleanSource, cleanTarget)
	cleanPush, err := merge.PushMerge(ctx, ref, "merge-lifecycle-clean", MergeWorkspace{
		SourceBranch: "feature-clean", TargetBranch: "main", SourceRevision: cleanSource,
		TargetRevision: cleanTarget, Message: "Merge clean feature",
	}, clean.StagedRevision, readCredential, writeCredential, authorizer)
	if err != nil {
		t.Fatalf("clean PushMerge returned an error: %v", err)
	}
	if cleanPush.RemoteRevision == "" {
		t.Fatalf("clean push returned no remote revision: %#v", cleanPush)
	}
	if got := authorizationCalls.Load(); got != 1 {
		t.Fatalf("clean authorization callback count = %d, want 1", got)
	}
	duplicatePush, err := merge.PushMerge(ctx, ref, "merge-lifecycle-clean", MergeWorkspace{
		SourceBranch: "feature-clean", TargetBranch: "main", SourceRevision: cleanSource,
		TargetRevision: cleanTarget, Message: "Merge clean feature",
	}, clean.StagedRevision, readCredential, writeCredential, authorizer)
	if err != nil {
		t.Fatalf("duplicate clean PushMerge returned an error: %v", err)
	}
	if duplicatePush.RemoteRevision != cleanPush.RemoteRevision {
		t.Fatalf("duplicate clean push changed remote revision: first=%#v second=%#v", cleanPush, duplicatePush)
	}
	if got := authorizationCalls.Load(); got != 1 {
		t.Fatalf("already-pushed authorization callback count = %d, want 1", got)
	}
	if err := merge.CleanupMergeWorkspace(ctx, ref, "merge-lifecycle-clean"); err != nil {
		t.Fatalf("clean workspace cleanup returned an error: %v", err)
	}

	testSourceAndTargetRaces(t, ctx, merge, client, ref, readCredential, writeCredential, authorizer, cliPath,
		worktree, identity)
	testWorkspaceRecovery(t, ctx, cacheDirectory, client, ref, readCredential, writeCredential, authorizer, identity)

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
		conflictSource, conflictTarget, "Conflict abort", writeCredential)
	if err != nil {
		t.Fatalf("conflict StartMerge returned an error: %v", err)
	}
	if len(abortStart.Conflicts) == 0 {
		t.Fatalf("conflict merge did not report conflicts: %#v", abortStart)
	}
	abortWorkspace := MergeWorkspace{SourceBranch: "feature-conflict", TargetBranch: "conflict-target",
		SourceRevision: conflictSource, TargetRevision: conflictTarget, Message: "Conflict abort"}
	listed, err := merge.ListConflicts(ctx, ref, abortOperation, abortWorkspace, nil, writeCredential)
	if err != nil || len(listed) == 0 {
		t.Fatalf("ListConflicts returned %v and %#v", err, listed)
	}
	if err := merge.AbortMerge(ctx, ref, abortOperation, writeCredential); err != nil {
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
		restartSource, restartTarget, "Conflict restart", writeCredential)
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
	restartResult, err := merge.RestartMerge(ctx, ref, restartOperation, MergeWorkspace{
		SourceBranch: "feature-conflict", TargetBranch: "conflict-target", SourceRevision: restartSource,
		TargetRevision: newTarget, Message: "Conflict restart",
	}, nil, writeCredential)
	if err != nil {
		t.Fatalf("RestartMerge returned an error: %v", err)
	}
	if len(restartResult.Conflicts) == 0 {
		t.Fatalf("RestartMerge lost the expected conflict: %#v", restartResult)
	}
	restartWorkspace := MergeWorkspace{SourceBranch: "feature-conflict", TargetBranch: "conflict-target",
		SourceRevision: restartSource, TargetRevision: newTarget, Message: "Conflict restart",
		Resolutions: []MergeResolution{{Path: restartResult.Conflicts[0], Strategy: "theirs"}}}
	if _, err := merge.ResolveMerge(ctx, ref, restartOperation, restartWorkspace, restartResult.Conflicts,
		"theirs", writeCredential); err != nil {
		t.Fatalf("ResolveMerge returned an error: %v", err)
	}
	remaining, err := merge.ListConflicts(ctx, ref, restartOperation, restartWorkspace, nil, writeCredential)
	if err != nil {
		t.Fatalf("ListConflicts after resolution returned an error: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("resolved merge still has conflicts: %#v", remaining)
	}
	restartedPush, err := merge.PushMerge(ctx, ref, restartOperation, restartWorkspace,
		restartResult.StagedRevision, readCredential, writeCredential, authorizer)
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

func testSourceAndTargetRaces(
	t *testing.T,
	ctx context.Context,
	merge MergeClient,
	client *SDKClient,
	ref RepositoryRef,
	readCredential Credential,
	writeCredential Credential,
	authorizer PushAuthorizer,
	cliPath string,
	worktree string,
	identity string,
) {
	t.Helper()
	source, err := fixtureBranchRevision(ctx, client, ref, "race-source", identity)
	if err != nil {
		t.Fatal(err)
	}
	target, err := fixtureBranchRevision(ctx, client, ref, "race-target", identity)
	if err != nil {
		t.Fatal(err)
	}
	operation := "merge-lifecycle-source-race"
	workspace := MergeWorkspace{SourceBranch: "race-source", TargetBranch: "race-target",
		SourceRevision: source, TargetRevision: target, Message: "Source race"}
	started, err := merge.StartMerge(ctx, ref, operation, workspace.SourceBranch, workspace.TargetBranch,
		source, target, workspace.Message, writeCredential)
	if err != nil {
		t.Fatalf("source race StartMerge returned an error: %v", err)
	}
	assertMergeParents(t, started.Parents, source, target)
	advanceFixtureBranch(t, ctx, cliPath, worktree, identity, "race-source", "race-source-next.txt",
		"source advanced", "advance race source")
	if _, err := merge.PushMerge(ctx, ref, operation, workspace, started.StagedRevision,
		readCredential, writeCredential, authorizer); !errors.Is(err, ErrMergeStale) {
		t.Fatalf("source race PushMerge error = %v, want ErrMergeStale", err)
	}
	if err := merge.CleanupMergeWorkspace(ctx, ref, operation); err != nil {
		t.Fatalf("source race workspace cleanup: %v", err)
	}

	source, err = fixtureBranchRevision(ctx, client, ref, "race-source", identity)
	if err != nil {
		t.Fatal(err)
	}
	target, err = fixtureBranchRevision(ctx, client, ref, "race-target", identity)
	if err != nil {
		t.Fatal(err)
	}
	operation = "merge-lifecycle-target-race"
	workspace = MergeWorkspace{SourceBranch: "race-source", TargetBranch: "race-target",
		SourceRevision: source, TargetRevision: target, Message: "Target race"}
	started, err = merge.StartMerge(ctx, ref, operation, workspace.SourceBranch, workspace.TargetBranch,
		source, target, workspace.Message, writeCredential)
	if err != nil {
		t.Fatalf("target race StartMerge returned an error: %v", err)
	}
	assertMergeParents(t, started.Parents, source, target)
	advanceFixtureBranch(t, ctx, cliPath, worktree, identity, "race-target", "race-target-next.txt",
		"target advanced", "advance race target")
	if _, err := merge.PushMerge(ctx, ref, operation, workspace, started.StagedRevision,
		readCredential, writeCredential, authorizer); !errors.Is(err, ErrMergeStale) {
		t.Fatalf("target race PushMerge error = %v, want ErrMergeStale", err)
	}
	if err := merge.CleanupMergeWorkspace(ctx, ref, operation); err != nil {
		t.Fatalf("target race workspace cleanup: %v", err)
	}
}

func testWorkspaceRecovery(
	t *testing.T,
	ctx context.Context,
	cacheDirectory string,
	client *SDKClient,
	ref RepositoryRef,
	readCredential Credential,
	writeCredential Credential,
	authorizer PushAuthorizer,
	identity string,
) {
	t.Helper()
	source, err := fixtureBranchRevision(ctx, client, ref, "recovery-source", identity)
	if err != nil {
		t.Fatal(err)
	}
	target, err := fixtureBranchRevision(ctx, client, ref, "recovery-target", identity)
	if err != nil {
		t.Fatal(err)
	}
	operation := "merge-lifecycle-workspace-recovery"
	workspace := MergeWorkspace{SourceBranch: "recovery-source", TargetBranch: "recovery-target",
		SourceRevision: source, TargetRevision: target, Message: "Workspace recovery"}
	started, err := client.StartMerge(ctx, ref, operation, workspace.SourceBranch, workspace.TargetBranch,
		source, target, workspace.Message, writeCredential)
	if err != nil {
		t.Fatalf("recovery StartMerge returned an error: %v", err)
	}
	if len(started.Conflicts) == 0 {
		t.Fatalf("recovery StartMerge did not report conflicts: %#v", started)
	}
	resolution, err := client.ResolveMerge(ctx, ref, operation, workspace, started.Conflicts,
		"theirs", writeCredential)
	if err != nil {
		t.Fatalf("recovery ResolveMerge returned an error: %v", err)
	}
	if resolution.StagedRevision == "" || !sameMergeParents(resolution.Parents, source, target) {
		t.Fatalf("resolved merge did not persist the exact merge commit parents: %#v", resolution)
	}
	workspace.Resolutions = make([]MergeResolution, 0, len(started.Conflicts))
	for _, path := range started.Conflicts {
		workspace.Resolutions = append(workspace.Resolutions, MergeResolution{Path: path, Strategy: "theirs"})
	}
	transportRef, err := client.transportRepositoryRef(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	path, err := client.operationPath(transportRef, operation)
	if err != nil {
		t.Fatal(err)
	}
	if err := removeMergeWorkspace(ctx, path); err != nil {
		t.Fatalf("delete recovery workspace: %v", err)
	}
	recoveredClient, err := NewDevelopmentSDKClient(cacheDirectory)
	if err != nil {
		t.Fatal(err)
	}
	recoveredMerge := MergeClient(recoveredClient)
	remaining, err := recoveredMerge.ListConflicts(ctx, ref, operation, workspace, nil, writeCredential)
	if err != nil {
		t.Fatalf("recovered ListConflicts returned an error: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("stored conflict resolutions were not replayed: %#v", remaining)
	}
	pushed, err := recoveredMerge.PushMerge(ctx, ref, operation, workspace, resolution.StagedRevision,
		readCredential, writeCredential, authorizer)
	if err != nil {
		t.Fatalf("recovered PushMerge returned an error: %v", err)
	}
	if pushed.RemoteRevision == "" {
		t.Fatalf("recovered push returned no remote revision: %#v", pushed)
	}
	if err := recoveredMerge.CleanupMergeWorkspace(ctx, ref, operation); err != nil {
		t.Fatalf("recovery workspace cleanup: %v", err)
	}
}

func assertMergeParents(t *testing.T, parents []string, source, target string) {
	t.Helper()
	if !sameMergeParents(parents, source, target) {
		t.Fatalf("merge parents = %#v, want %q and %q", parents, source, target)
	}
}

func fixtureBranchRevision(
	ctx context.Context,
	client *SDKClient,
	ref RepositoryRef,
	branch string,
	identity string,
) (string, error) {
	branches, err := client.Branches(ctx, ref, developmentCredential(ref.LoreRepositoryID, identity, ScopeRead))
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
	advanceFixtureBranch(t, ctx, cliPath, worktree, identity, "conflict-target", "restart-target.txt",
		"target changed", "advance target")
}

func advanceFixtureBranch(
	t *testing.T,
	ctx context.Context,
	cliPath string,
	worktree string,
	identity string,
	branch string,
	file string,
	value string,
	message string,
) {
	t.Helper()
	runFixtureCLI(t, ctx, cliPath, "--repository", worktree, "--identity", identity,
		"branch", "switch", branch, "--reset")
	if err := os.WriteFile(filepath.Join(worktree, file), []byte(value+"\n"), 0o644); err != nil {
		t.Fatalf("write %s fixture change: %v", branch, err)
	}
	runFixtureCLI(t, ctx, cliPath, "--repository", worktree, "stage", "--scan", ".")
	runFixtureCLI(t, ctx, cliPath, "--repository", worktree, "--identity", identity, "commit", message)
	runFixtureCLI(t, ctx, cliPath, "--repository", worktree, "--identity", identity, "push", branch)
}

func runFixtureCLI(t *testing.T, ctx context.Context, cliPath string, args ...string) {
	t.Helper()
	command := exec.CommandContext(ctx, cliPath, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Lore fixture command %q failed: %v\n%s", strings.Join(args, " "), err, output)
	}
}
