package lore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	loresdk "github.com/EpicGames/lore-go"
	"github.com/EpicGames/lore-go/types"
)

var (
	ErrMergeStale       = errors.New("Lore merge revisions are stale")
	ErrMergeParentCheck = errors.New("Lore merge commit parents do not match the requested revisions")
	ErrMergeNoWorkspace = errors.New("Lore merge workspace is unavailable")
)

func (client *SDKClient) operationPath(repository RepositoryRef, operationID string) (string, error) {
	if operationID == "" || len(operationID) > 128 || strings.ContainsAny(operationID, `/\\`) {
		return "", errors.New("invalid Lore merge operation ID")
	}
	if _, err := client.cachePath(repository.CacheKey); err != nil {
		return "", err
	}
	return filepath.Join(client.cacheDirectory, "operations", repository.CacheKey, operationID), nil
}

func (client *SDKClient) workspaceExists(repository RepositoryRef, operationID string) (bool, error) {
	path, err := client.operationPath(repository, operationID)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(filepath.Join(path, ".lore", "config.toml"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect Lore merge workspace: %w", err)
	}
	return true, nil
}

func (client *SDKClient) ensureWorkingClone(
	ctx context.Context,
	repository RepositoryRef,
	operationID string,
	targetRevision string,
	credential Credential,
) (string, error) {
	path, err := client.operationPath(repository, operationID)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(path, ".lore", "config.toml")); err == nil {
		return path, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect Lore merge workspace: %w", err)
	}
	if targetRevision == "" {
		return "", ErrMergeNoWorkspace
	}
	if err := os.MkdirAll(path, 0o750); err != nil {
		return "", fmt.Errorf("create Lore merge workspace: %w", err)
	}
	return path, client.cloneWorkspace(ctx, path, repository, targetRevision, credential)
}

func (client *SDKClient) cloneWorkspace(
	ctx context.Context,
	path string,
	repository RepositoryRef,
	targetRevision string,
	credential Credential,
) error {
	globals, cleanupGlobals := types.NewLoreGlobalArgs(types.LoreGlobalArgs{
		RepositoryPath: path,
		Identity:       credential.Identity,
		Remote:         true,
		Cache:          true,
	})
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreRepositoryCloneArgs(types.LoreRepositoryCloneArgs{
		RepositoryUrl: repository.URL,
		Bare:          false,
	})
	defer cleanupArgs()
	if err := waitLore(ctx, loresdk.RepositoryClone(&globals, &args).Wait); err != nil {
		return fmt.Errorf("create Lore merge workspace: %w", err)
	}
	return nil
}

func (client *SDKClient) withWorkspace(
	ctx context.Context,
	repository RepositoryRef,
	operationID string,
	targetRevision string,
	credential Credential,
	fn func(string) error,
) error {
	path, err := client.operationPath(repository, operationID)
	if err != nil {
		return err
	}
	lockValue, _ := client.locks.LoadOrStore(path, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	if err := client.authenticate(ctx, path, repository.URL, credential); err != nil {
		return err
	}
	if _, err := client.ensureWorkingClone(ctx, repository, operationID, targetRevision, credential); err != nil {
		return err
	}
	return fn(path)
}

func (client *SDKClient) StartMerge(
	ctx context.Context,
	repository RepositoryRef,
	operationID string,
	sourceBranch string,
	targetBranch string,
	sourceRevision string,
	targetRevision string,
	message string,
	credential Credential,
) (MergeStartResult, error) {
	if err := ValidateCredential(repository, credential, ScopeWrite); err != nil {
		return MergeStartResult{}, err
	}
	readCredential := credential
	readCredential.Scope = ScopeRead
	branches, err := client.Branches(ctx, repository, readCredential)
	if err != nil {
		return MergeStartResult{}, err
	}
	currentSource, sourceFound := branchLatestRevision(branches, sourceBranch)
	currentTarget, targetFound := branchLatestRevision(branches, targetBranch)
	if !sourceFound || !targetFound || currentSource != sourceRevision || currentTarget != targetRevision {
		return MergeStartResult{}, ErrMergeStale
	}
	if err := client.CleanupMergeWorkspace(ctx, repository, operationID); err != nil {
		return MergeStartResult{}, err
	}
	workspace := MergeWorkspace{
		SourceBranch: sourceBranch, TargetBranch: targetBranch,
		SourceRevision: sourceRevision, TargetRevision: targetRevision, Message: message,
	}
	var result MergeStartResult
	err = client.withWorkspace(ctx, repository, operationID, targetRevision, credential, func(path string) error {
		var err error
		result, err = client.mergeInWorkspace(ctx, path, workspace, nil, credential)
		return err
	})
	if err != nil {
		_ = client.CleanupMergeWorkspace(context.WithoutCancel(ctx), repository, operationID)
		return MergeStartResult{}, err
	}
	return result, nil
}

func (client *SDKClient) EnsureMergeWorkspace(
	ctx context.Context,
	repository RepositoryRef,
	operationID string,
	workspace MergeWorkspace,
	credential Credential,
) error {
	if credential.Scope != ScopeRead && credential.Scope != ScopeWrite {
		return errors.New("Lore merge workspace requires a repository read or write credential")
	}
	if err := ValidateCredential(repository, credential, credential.Scope); err != nil {
		return err
	}
	path, err := client.operationPath(repository, operationID)
	if err != nil {
		return err
	}
	lockValue, _ := client.locks.LoadOrStore(path, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	if err := client.authenticate(ctx, path, repository.URL, credential); err != nil {
		return err
	}
	if exists, err := client.workspaceExists(repository, operationID); err != nil {
		return err
	} else if exists {
		return nil
	}
	if err := removeMergeWorkspace(context.WithoutCancel(ctx), path); err != nil {
		return fmt.Errorf("reset missing Lore merge workspace: %w", err)
	}
	if err := os.MkdirAll(path, 0o750); err != nil {
		return fmt.Errorf("create Lore merge workspace: %w", err)
	}
	if err := client.cloneWorkspace(ctx, path, repository, workspace.TargetRevision, credential); err != nil {
		_ = removeMergeWorkspace(context.WithoutCancel(ctx), path)
		return err
	}
	if _, err := client.rebuildMergeState(ctx, path, workspace, nil, credential); err != nil {
		_ = removeMergeWorkspace(context.WithoutCancel(ctx), path)
		return err
	}
	return nil
}

func (client *SDKClient) mergeInWorkspace(
	ctx context.Context,
	path string,
	workspace MergeWorkspace,
	restartPaths []string,
	credential Credential,
) (MergeStartResult, error) {
	identity := credential.Identity
	if err := switchExactRevision(ctx, path, workspace.SourceBranch, workspace.SourceRevision, identity); err != nil {
		return MergeStartResult{}, fmt.Errorf("anchor Lore source revision: %w", err)
	}
	if err := switchExactRevision(ctx, path, workspace.TargetBranch, workspace.TargetRevision, identity); err != nil {
		return MergeStartResult{}, fmt.Errorf("anchor Lore target revision: %w", err)
	}
	currentTarget, _, err := workspaceRevision(ctx, path, identity, true)
	if err != nil {
		return MergeStartResult{}, err
	}
	if currentTarget != workspace.TargetRevision {
		return MergeStartResult{}, fmt.Errorf("%w: target anchor is %s", ErrMergeParentCheck, currentTarget)
	}
	result := MergeStartResult{SourceRevision: workspace.SourceRevision, TargetRevision: workspace.TargetRevision}
	var anchoredSource string
	globals, cleanupGlobals := readGlobals(path, identity)
	args, cleanupArgs := types.NewLoreBranchMergeStartArgs(types.LoreBranchMergeStartArgs{
		Branch: workspace.SourceBranch, Message: workspace.Message, NoCommit: true,
	})
	op := loresdk.BranchMergeStart(&globals, &args)
	op.Callback(func(event *types.LoreEventFFI, _ uint64) {
		switch event.Tag {
		case types.LoreEventTag_BRANCH_MERGE_START_BEGIN:
			if data, ok := event.GetData().(*types.LoreBranchMergeStartBeginEventDataFFI); ok {
				anchoredSource = data.Revision.String()
			}
		case types.LoreEventTag_BRANCH_MERGE_CONFLICT_FILE:
			if data, ok := event.GetData().(*types.LoreBranchMergeConflictFileEventDataFFI); ok {
				result.Conflicts = appendUnique(result.Conflicts, data.Path.String())
			}
		case types.LoreEventTag_BRANCH_MERGE_START_END:
			if data, ok := event.GetData().(*types.LoreBranchMergeStartEndEventDataFFI); ok {
				if signature := data.Signature.String(); !isZeroRevision(signature) {
					result.StagedRevision = signature
				}
			}
		}
	})
	err = waitLore(ctx, op.Wait)
	cleanupArgs()
	cleanupGlobals()
	if err != nil {
		return MergeStartResult{}, fmt.Errorf("start Lore merge: %w", err)
	}
	if anchoredSource != workspace.SourceRevision {
		return MergeStartResult{}, fmt.Errorf("%w: source anchor is %s", ErrMergeParentCheck, anchoredSource)
	}
	if len(restartPaths) > 0 {
		if err := restartWorkspacePaths(ctx, path, restartPaths, identity); err != nil {
			return MergeStartResult{}, err
		}
	}
	if len(result.Conflicts) > 0 {
		return result, nil
	}
	if len(result.Conflicts) == 0 {
		committed, err := commitWorkspace(ctx, path, identity, workspace.Message)
		if err != nil {
			return MergeStartResult{}, err
		}
		result.StagedRevision = committed
	}
	parents, err := workspaceRevisionParents(ctx, path, result.StagedRevision, identity)
	if err != nil {
		return MergeStartResult{}, err
	}
	result.Parents = parents
	if !sameMergeParents(parents, workspace.SourceRevision, workspace.TargetRevision) {
		return MergeStartResult{}, fmt.Errorf("%w: got %v", ErrMergeParentCheck, parents)
	}
	return result, nil
}

func (client *SDKClient) rebuildMergeState(
	ctx context.Context,
	path string,
	workspace MergeWorkspace,
	restartPaths []string,
	credential Credential,
) (MergeStartResult, error) {
	result, err := client.mergeInWorkspace(ctx, path, workspace, restartPaths, credential)
	if err != nil {
		return MergeStartResult{}, err
	}
	if len(result.Conflicts) == 0 {
		return result, nil
	}
	for _, resolution := range workspace.Resolutions {
		if _, err := resolveWorkspacePaths(ctx, path, resolution.Path, resolution.Strategy, credential.Identity); err != nil {
			return MergeStartResult{}, err
		}
	}
	conflicts, err := listWorkspaceConflicts(ctx, path, credential.Identity)
	if err != nil {
		return MergeStartResult{}, err
	}
	result.Conflicts = conflicts
	if len(conflicts) > 0 {
		return result, nil
	}
	committed, err := commitWorkspace(ctx, path, credential.Identity, workspace.Message)
	if err != nil {
		return MergeStartResult{}, err
	}
	result.StagedRevision = committed
	parents, err := workspaceRevisionParents(ctx, path, result.StagedRevision, credential.Identity)
	if err != nil {
		return MergeStartResult{}, err
	}
	result.Parents = parents
	if !sameMergeParents(parents, workspace.SourceRevision, workspace.TargetRevision) {
		return MergeStartResult{}, fmt.Errorf("%w: got %v", ErrMergeParentCheck, parents)
	}
	return result, nil
}

func switchExactRevision(ctx context.Context, path, branch, revision, identity string) error {
	globals, cleanupGlobals := readGlobals(path, identity)
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreBranchSwitchArgs(types.LoreBranchSwitchArgs{
		Branch: branch, Revision: revision, Reset: true,
	})
	if err := waitLore(ctx, loresdk.BranchSwitch(&globals, &args).Wait); err != nil {
		cleanupArgs()
		return err
	}
	cleanupArgs()
	resetArgs, cleanupResetArgs := types.NewLoreBranchResetArgs(types.LoreBranchResetArgs{
		Branch: branch, Revision: revision,
	})
	defer cleanupResetArgs()
	if err := waitLore(ctx, loresdk.BranchReset(&globals, &resetArgs).Wait); err != nil {
		return fmt.Errorf("reset Lore merge branch anchor: %w", err)
	}
	return nil
}

func restartWorkspacePaths(ctx context.Context, path string, paths []string, identity string) error {
	globals, cleanupGlobals := readGlobals(path, identity)
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreBranchMergeRestartArgs(types.LoreBranchMergeRestartArgs{
		Paths: mergeWorkspacePaths(path, paths),
	})
	defer cleanupArgs()
	if err := waitLore(ctx, loresdk.BranchMergeRestart(&globals, &args).Wait); err != nil {
		return fmt.Errorf("restart Lore merge materialization: %w", err)
	}
	return nil
}

func (client *SDKClient) ResolveMerge(
	ctx context.Context,
	repository RepositoryRef,
	operationID string,
	workspace MergeWorkspace,
	paths []string,
	strategy string,
	credential Credential,
) (MergeStartResult, error) {
	if err := ValidateCredential(repository, credential, ScopeWrite); err != nil {
		return MergeStartResult{}, err
	}
	if err := validateMergeWorkspacePaths(paths); err != nil {
		return MergeStartResult{}, err
	}
	if err := client.EnsureMergeWorkspace(ctx, repository, operationID, workspace, credential); err != nil {
		return MergeStartResult{}, err
	}
	result := MergeStartResult{
		SourceRevision: workspace.SourceRevision,
		TargetRevision: workspace.TargetRevision,
	}
	err := client.withWorkspace(ctx, repository, operationID, "", credential, func(path string) error {
		revision, err := resolveWorkspacePaths(ctx, path, strings.Join(paths, "\x00"), strategy, credential.Identity)
		if err != nil {
			return err
		}
		result.StagedRevision = revision
		conflicts, err := listWorkspaceConflicts(ctx, path, credential.Identity)
		if err != nil {
			return err
		}
		result.Conflicts = conflicts
		if len(conflicts) > 0 {
			return nil
		}
		committed, err := commitWorkspace(ctx, path, credential.Identity, workspace.Message)
		if err != nil {
			return err
		}
		result.StagedRevision = committed
		parents, err := workspaceRevisionParents(ctx, path, committed, credential.Identity)
		if err != nil {
			return err
		}
		result.Parents = parents
		if !sameMergeParents(parents, workspace.SourceRevision, workspace.TargetRevision) {
			return fmt.Errorf("%w: got %v", ErrMergeParentCheck, parents)
		}
		return nil
	})
	return result, err
}

func resolveWorkspacePaths(ctx context.Context, path, encodedPaths, strategy, identity string) (string, error) {
	paths := strings.Split(encodedPaths, "\x00")
	if encodedPaths == "" {
		paths = nil
	}
	if err := validateMergeWorkspacePaths(paths); err != nil {
		return "", err
	}
	workspacePaths := mergeWorkspacePaths(path, paths)
	globals, cleanupGlobals := readGlobals(path, identity)
	defer cleanupGlobals()
	var revision string
	if strategy == "mine" {
		args, cleanup := types.NewLoreBranchMergeResolveMineArgs(types.LoreBranchMergeResolveMineArgs{
			Paths: workspacePaths,
		})
		defer cleanup()
		op := loresdk.BranchMergeResolveMine(&globals, &args)
		op.Callback(func(event *types.LoreEventFFI, _ uint64) {
			if event.Tag == types.LoreEventTag_BRANCH_MERGE_RESOLVE_REVISION {
				if data, ok := event.GetData().(*types.LoreBranchMergeResolveRevisionEventDataFFI); ok {
					revision = data.Revision.String()
				}
			}
		})
		if err := waitLore(ctx, op.Wait); err != nil {
			return "", fmt.Errorf("resolve Lore merge with mine: %w", err)
		}
		return revision, nil
	}
	if strategy == "theirs" {
		args, cleanup := types.NewLoreBranchMergeResolveTheirsArgs(types.LoreBranchMergeResolveTheirsArgs{
			Paths: workspacePaths,
		})
		defer cleanup()
		op := loresdk.BranchMergeResolveTheirs(&globals, &args)
		op.Callback(func(event *types.LoreEventFFI, _ uint64) {
			if event.Tag == types.LoreEventTag_BRANCH_MERGE_RESOLVE_REVISION {
				if data, ok := event.GetData().(*types.LoreBranchMergeResolveRevisionEventDataFFI); ok {
					revision = data.Revision.String()
				}
			}
		})
		if err := waitLore(ctx, op.Wait); err != nil {
			return "", fmt.Errorf("resolve Lore merge with theirs: %w", err)
		}
		return revision, nil
	}
	return "", errors.New("merge conflict strategy must be mine or theirs")
}

func (client *SDKClient) ListConflicts(
	ctx context.Context,
	repository RepositoryRef,
	operationID string,
	workspace MergeWorkspace,
	paths []string,
	credential Credential,
) ([]string, error) {
	if credential.Scope != ScopeRead && credential.Scope != ScopeWrite {
		return nil, errors.New("Lore conflict listing requires a repository read or write credential")
	}
	if err := ValidateCredential(repository, credential, credential.Scope); err != nil {
		return nil, err
	}
	if err := validateMergeWorkspacePaths(paths); err != nil {
		return nil, err
	}
	if err := client.EnsureMergeWorkspace(ctx, repository, operationID, workspace, credential); err != nil {
		return nil, err
	}
	var conflicts []string
	err := client.withWorkspace(ctx, repository, operationID, "", credential, func(path string) error {
		var err error
		conflicts, err = listWorkspaceConflicts(ctx, path, credential.Identity)
		return err
	})
	return conflicts, err
}

func listWorkspaceConflicts(ctx context.Context, path, identity string) ([]string, error) {
	globals, cleanupGlobals := readGlobals(path, identity)
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreRepositoryStatusArgs(types.LoreRepositoryStatusArgs{Staged: true})
	defer cleanupArgs()
	conflicts := make([]string, 0)
	op := loresdk.RepositoryStatus(&globals, &args)
	op.Callback(func(event *types.LoreEventFFI, _ uint64) {
		if event.Tag != types.LoreEventTag_REPOSITORY_STATUS_FILE || len(conflicts) >= maxConflictPaths {
			return
		}
		data, ok := event.GetData().(*types.LoreRepositoryStatusFileEventDataFFI)
		if ok && data.FlagConflictUnresolved != 0 {
			conflicts = appendUnique(conflicts, data.Path.String())
		}
	})
	if err := waitLore(ctx, op.Wait); err != nil {
		return nil, fmt.Errorf("read Lore merge conflicts: %w", err)
	}
	return conflicts, nil
}

func (client *SDKClient) AbortMerge(
	ctx context.Context,
	repository RepositoryRef,
	operationID string,
	credential Credential,
) error {
	if err := ValidateCredential(repository, credential, ScopeWrite); err != nil {
		return err
	}
	exists, err := client.workspaceExists(repository, operationID)
	if err != nil || !exists {
		return err
	}
	return client.withWorkspace(ctx, repository, operationID, "", credential, func(path string) error {
		globals, cleanupGlobals := readGlobals(path, credential.Identity)
		defer cleanupGlobals()
		args, cleanupArgs := types.NewLoreBranchMergeAbortArgs(types.LoreBranchMergeAbortArgs{})
		defer cleanupArgs()
		return waitLore(ctx, loresdk.BranchMergeAbort(&globals, &args).Wait)
	})
}

func (client *SDKClient) RestartMerge(
	ctx context.Context,
	repository RepositoryRef,
	operationID string,
	workspace MergeWorkspace,
	paths []string,
	credential Credential,
) (MergeStartResult, error) {
	if err := ValidateCredential(repository, credential, ScopeWrite); err != nil {
		return MergeStartResult{}, err
	}
	if err := validateMergeWorkspacePaths(paths); err != nil {
		return MergeStartResult{}, err
	}
	if err := client.CleanupMergeWorkspace(ctx, repository, operationID); err != nil {
		return MergeStartResult{}, err
	}
	path, err := client.operationPath(repository, operationID)
	if err != nil {
		return MergeStartResult{}, err
	}
	lockValue, _ := client.locks.LoadOrStore(path, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	if err := client.authenticate(ctx, path, repository.URL, credential); err != nil {
		return MergeStartResult{}, err
	}
	if err := os.MkdirAll(path, 0o750); err != nil {
		return MergeStartResult{}, err
	}
	if err := client.cloneWorkspace(ctx, path, repository, workspace.TargetRevision, credential); err != nil {
		_ = removeMergeWorkspace(context.WithoutCancel(ctx), path)
		return MergeStartResult{}, err
	}
	result, err := client.rebuildMergeState(ctx, path, workspace, paths, credential)
	if err != nil {
		_ = removeMergeWorkspace(context.WithoutCancel(ctx), path)
		return MergeStartResult{}, err
	}
	result.Conflicts, err = listWorkspaceConflicts(ctx, path, credential.Identity)
	if err != nil {
		return MergeStartResult{}, err
	}
	return result, nil
}

func (client *SDKClient) PushMerge(
	ctx context.Context,
	repository RepositoryRef,
	operationID string,
	workspace MergeWorkspace,
	stagedRevision string,
	readCredential Credential,
	writeCredential Credential,
	authorizer PushAuthorizer,
) (MergePushResult, error) {
	if err := ValidateCredential(repository, readCredential, ScopeRead); err != nil {
		return MergePushResult{}, err
	}
	if err := ValidateCredential(repository, writeCredential, ScopeWrite); err != nil {
		return MergePushResult{}, err
	}
	var result MergePushResult
	if err := client.checkRemoteMergeRevisions(
		ctx, repository, workspace, stagedRevision, readCredential, &result,
	); err != nil {
		return MergePushResult{}, err
	}
	if stagedRevision != "" && result.RemoteTargetRevision == stagedRevision {
		result.LocalRevision = stagedRevision
		return result, nil
	}
	if err := client.EnsureMergeWorkspace(ctx, repository, operationID, workspace, writeCredential); err != nil {
		return MergePushResult{}, err
	}
	err := client.withWorkspace(ctx, repository, operationID, "", writeCredential, func(path string) error {
		if err := client.checkRemoteMergeRevisions(
			ctx, repository, workspace, stagedRevision, readCredential, &result,
		); err != nil {
			return err
		}
		if stagedRevision != "" && result.RemoteTargetRevision == stagedRevision {
			result.LocalRevision = stagedRevision
			return nil
		}
		conflicts, err := listWorkspaceConflicts(ctx, path, writeCredential.Identity)
		if err != nil {
			return err
		}
		if len(conflicts) > 0 {
			return fmt.Errorf("Lore merge still has %d unresolved conflicts", len(conflicts))
		}
		localRevision, err := workspaceCurrentRevision(ctx, path, writeCredential.Identity)
		if err != nil {
			return err
		}
		staged, err := workspaceStagedRevision(ctx, path, writeCredential.Identity)
		if err != nil {
			return err
		}
		if staged != "" {
			localRevision, err = commitWorkspace(ctx, path, writeCredential.Identity, workspace.Message)
			if err != nil {
				return err
			}
		}
		if localRevision == "" {
			return errors.New("Lore merge workspace has no local revision")
		}
		parents, err := workspaceRevisionParents(ctx, path, localRevision, writeCredential.Identity)
		if err != nil {
			return err
		}
		if !sameMergeParents(parents, workspace.SourceRevision, workspace.TargetRevision) {
			return fmt.Errorf("%w: got %v", ErrMergeParentCheck, parents)
		}
		if stagedRevision != "" && localRevision != stagedRevision && len(workspace.Resolutions) == 0 {
			return fmt.Errorf("%w: local proposed revision is %s, expected %s", ErrMergeParentCheck,
				localRevision, stagedRevision)
		}
		result.LocalRevision = localRevision
		result.Parents = parents
		remoteRecoveryRevision := ""
		if localRevision == stagedRevision {
			remoteRecoveryRevision = stagedRevision
		}
		if err := client.checkRemoteMergeRevisions(
			ctx, repository, workspace, remoteRecoveryRevision, readCredential, &result,
		); err != nil {
			return err
		}
		if remoteRecoveryRevision != "" && result.RemoteTargetRevision == localRevision {
			result.RemoteRevision = localRevision
			return nil
		}
		if result.TargetBranchID == "" {
			return fmt.Errorf("%w: Lore target branch has no stable ID", ErrPushAuthorizationDenied)
		}
		branchID, branchName, branchRevision, err := workspaceBranchState(
			ctx, path, writeCredential.Identity,
		)
		if err != nil {
			return err
		}
		if branchID != result.TargetBranchID || branchName != workspace.TargetBranch || branchRevision != localRevision {
			return fmt.Errorf("%w: local branch does not match target", ErrMergeParentCheck)
		}
		if err := client.checkRemoteMergeRevisions(ctx, repository, workspace, "", readCredential, &result); err != nil {
			return err
		}
		if authorizer == nil {
			return ErrPushAuthorizationRequired
		}
		authorization := PushAuthorization{
			RepositoryPartition:    repository.CanonicalPartition(),
			OperationID:            operationID,
			TargetBranchID:         result.TargetBranchID,
			TargetBranchName:       workspace.TargetBranch,
			ExpectedTargetRevision: workspace.TargetRevision,
			ProposedRevision:       localRevision,
			SourceRevision:         workspace.SourceRevision,
			ParentRevisions:        append([]string(nil), parents...),
		}
		if err := authorizer.AuthorizeLoreMergePush(ctx, authorization); err != nil {
			if errors.Is(err, ErrPushAuthorizationDenied) {
				return ErrPushAuthorizationDenied
			}
			return fmt.Errorf("authorize Lore merge push: %w", err)
		}
		globals, cleanupGlobals := readGlobals(path, writeCredential.Identity)
		defer cleanupGlobals()
		args, cleanupArgs := types.NewLoreBranchPushArgs(types.LoreBranchPushArgs{
			FastForwardMerge: false,
		})
		defer cleanupArgs()
		op := loresdk.BranchPush(&globals, &args)
		op.Callback(func(event *types.LoreEventFFI, _ uint64) {
			switch event.Tag {
			case types.LoreEventTag_BRANCH_PUSH:
				if data, ok := event.GetData().(*types.LoreBranchPushEventDataFFI); ok {
					result.LocalRevision = data.LocalRevision.String()
					result.RemoteRevision = data.RemoteRevision.String()
				}
			case types.LoreEventTag_BRANCH_PUSH_REVISION_PUSH_END:
				if data, ok := event.GetData().(*types.LoreBranchPushRevisionPushEndEventDataFFI); ok {
					result.RemoteRevision = data.NewRemoteRevision.String()
				}
			}
		})
		if err := waitLore(ctx, op.Wait); err != nil {
			return fmt.Errorf("push Lore merge: %w", err)
		}
		if result.RemoteRevision == "" {
			return errors.New("Lore push response contained no remote revision")
		}
		return nil
	})
	return result, err
}

func (client *SDKClient) checkRemoteMergeRevisions(
	ctx context.Context,
	repository RepositoryRef,
	workspace MergeWorkspace,
	stagedRevision string,
	credential Credential,
	result *MergePushResult,
) error {
	branches, err := client.Branches(ctx, repository, credential)
	if err != nil {
		return err
	}
	for _, branch := range branches {
		if branch.Name == workspace.SourceBranch {
			result.RemoteSourceRevision = branch.LatestRevision
		}
		if branch.Name == workspace.TargetBranch {
			result.RemoteTargetRevision = branch.LatestRevision
			result.TargetBranchID = branch.ID
		}
	}
	result.RemoteRevision = result.RemoteTargetRevision
	if result.RemoteTargetRevision == stagedRevision && stagedRevision != "" {
		return nil
	}
	if result.RemoteSourceRevision != workspace.SourceRevision ||
		result.RemoteTargetRevision != workspace.TargetRevision {
		return fmt.Errorf("%w: source=%s target=%s", ErrMergeStale,
			result.RemoteSourceRevision, result.RemoteTargetRevision)
	}
	return nil
}

func (client *SDKClient) CleanupMergeWorkspace(
	ctx context.Context,
	repository RepositoryRef,
	operationID string,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := client.operationPath(repository, operationID)
	if err != nil {
		return err
	}
	lockValue, _ := client.locks.LoadOrStore(path, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	if err := removeMergeWorkspace(ctx, path); err != nil {
		return fmt.Errorf("clean Lore merge workspace: %w", err)
	}
	return nil
}

func removeMergeWorkspace(ctx context.Context, path string) error {
	var lastErr error
	for attempt := 0; attempt < 12; attempt++ {
		if err := os.RemoveAll(path); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
	return lastErr
}

func commitWorkspace(ctx context.Context, path, identity, message string) (string, error) {
	globals, cleanupGlobals := readGlobals(path, identity)
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreRevisionCommitArgs(types.LoreRevisionCommitArgs{Message: message})
	defer cleanupArgs()
	var revision string
	op := loresdk.RevisionCommit(&globals, &args)
	op.Callback(func(event *types.LoreEventFFI, _ uint64) {
		if event.Tag == types.LoreEventTag_REVISION_COMMIT_REVISION {
			if data, ok := event.GetData().(*types.LoreRevisionCommitRevisionEventDataFFI); ok {
				revision = data.Revision.String()
			}
		}
	})
	if err := waitLore(ctx, op.Wait); err != nil {
		return "", fmt.Errorf("commit Lore merge: %w", err)
	}
	if revision == "" {
		return "", errors.New("Lore merge commit response contained no revision")
	}
	return revision, nil
}

func workspaceRevisionParents(ctx context.Context, path, revision, identity string) ([]string, error) {
	if revision == "" {
		return nil, errors.New("Lore merge has no staged revision")
	}
	globals, cleanupGlobals := readGlobals(path, identity)
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreRevisionInfoArgs(types.LoreRevisionInfoArgs{Revision: revision})
	defer cleanupArgs()
	var parents []string
	op := loresdk.RevisionInfo(&globals, &args)
	op.Callback(func(event *types.LoreEventFFI, _ uint64) {
		if event.Tag == types.LoreEventTag_REVISION_INFO {
			if data, ok := event.GetData().(*types.LoreRevisionInfoEventDataFFI); ok {
				parents = hashStrings(data.Parent[:])
			}
		}
	})
	if err := waitLore(ctx, op.Wait); err != nil {
		return nil, fmt.Errorf("read Lore merge parents: %w", err)
	}
	return parents, nil
}

func workspaceRevision(ctx context.Context, path, identity string, staged bool) (string, string, error) {
	globals, cleanupGlobals := readGlobals(path, identity)
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreRepositoryStatusArgs(types.LoreRepositoryStatusArgs{Staged: staged})
	defer cleanupArgs()
	var current, stagedRevision string
	op := loresdk.RepositoryStatus(&globals, &args)
	op.Callback(func(event *types.LoreEventFFI, _ uint64) {
		if event.Tag != types.LoreEventTag_REPOSITORY_STATUS_REVISION {
			return
		}
		if data, ok := event.GetData().(*types.LoreRepositoryStatusRevisionEventDataFFI); ok {
			current = data.Revision.String()
			if !isZeroHash(data.RevisionStaged) {
				stagedRevision = data.RevisionStaged.String()
			}
		}
	})
	if err := waitLore(ctx, op.Wait); err != nil {
		return "", "", fmt.Errorf("read Lore merge status: %w", err)
	}
	return current, stagedRevision, nil
}

func workspaceCurrentRevision(ctx context.Context, path, identity string) (string, error) {
	current, _, err := workspaceRevision(ctx, path, identity, true)
	return current, err
}

func workspaceStagedRevision(ctx context.Context, path, identity string) (string, error) {
	_, staged, err := workspaceRevision(ctx, path, identity, true)
	return staged, err
}
