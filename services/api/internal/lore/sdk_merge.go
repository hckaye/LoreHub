package lore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	loresdk "github.com/EpicGames/lore-go"
	"github.com/EpicGames/lore-go/types"
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

func (client *SDKClient) ensureWorkingClone(
	ctx context.Context,
	repository RepositoryRef,
	operationID string,
	targetRevision string,
	identity string,
) (string, error) {
	path, err := client.operationPath(repository, operationID)
	if err != nil {
		return "", err
	}
	if _, err := filepath.Abs(path); err != nil {
		return "", fmt.Errorf("resolve Lore merge workspace: %w", err)
	}
	if _, err := os.Stat(filepath.Join(path, ".lore", "config.toml")); err == nil {
		return path, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect Lore merge workspace: %w", err)
	}
	if err := os.MkdirAll(path, 0o750); err != nil {
		return "", fmt.Errorf("create Lore merge workspace: %w", err)
	}
	globals, cleanupGlobals := types.NewLoreGlobalArgs(types.LoreGlobalArgs{
		RepositoryPath: path,
		Identity:       identity,
		Remote:         true,
		Cache:          true,
	})
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreRepositoryCloneArgs(types.LoreRepositoryCloneArgs{
		RepositoryUrl: repository.URL,
		Revision:      targetRevision,
		Bare:          false,
	})
	defer cleanupArgs()
	if err := waitLore(ctx, loresdk.RepositoryClone(&globals, &args).Wait); err != nil {
		return "", fmt.Errorf("create Lore merge workspace: %w", err)
	}
	return path, nil
}

func (client *SDKClient) withWorkspace(
	ctx context.Context,
	repository RepositoryRef,
	operationID string,
	targetRevision string,
	identity string,
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
	if _, err := client.ensureWorkingClone(ctx, repository, operationID, targetRevision, identity); err != nil {
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
	identity string,
) (MergeStartResult, error) {
	result := MergeStartResult{SourceRevision: sourceRevision, TargetRevision: targetRevision}
	err := client.withWorkspace(ctx, repository, operationID, targetRevision, identity, func(path string) error {
		globals, cleanupGlobals := readGlobals(path, identity)
		defer cleanupGlobals()
		sourceSwitchArgs, cleanupSourceSwitch := types.NewLoreBranchSwitchArgs(types.LoreBranchSwitchArgs{
			Branch: sourceBranch, Revision: sourceRevision, Reset: true,
		})
		if err := waitLore(ctx, loresdk.BranchSwitch(&globals, &sourceSwitchArgs).Wait); err != nil {
			cleanupSourceSwitch()
			return fmt.Errorf("switch Lore merge workspace to source revision: %w", err)
		}
		cleanupSourceSwitch()
		switchArgs, cleanupSwitch := types.NewLoreBranchSwitchArgs(types.LoreBranchSwitchArgs{
			Branch: sourceOrTarget(targetBranch), Revision: targetRevision, Reset: true,
		})
		if err := waitLore(ctx, loresdk.BranchSwitch(&globals, &switchArgs).Wait); err != nil {
			cleanupSwitch()
			return fmt.Errorf("switch Lore merge workspace to target: %w", err)
		}
		cleanupSwitch()
		mergeArgs, cleanupMerge := types.NewLoreBranchMergeStartArgs(types.LoreBranchMergeStartArgs{
			Branch: sourceBranch, Message: message, NoCommit: true,
		})
		defer cleanupMerge()
		op := loresdk.BranchMergeStart(&globals, &mergeArgs)
		op.Callback(func(event *types.LoreEventFFI, _ uint64) {
			switch event.Tag {
			case types.LoreEventTag_BRANCH_MERGE_CONFLICT_FILE:
				if data, ok := event.GetData().(*types.LoreBranchMergeConflictFileEventDataFFI); ok {
					result.Conflicts = appendUnique(result.Conflicts, data.Path.String())
				}
			case types.LoreEventTag_BRANCH_MERGE_START_END:
				if data, ok := event.GetData().(*types.LoreBranchMergeStartEndEventDataFFI); ok &&
					data.Signature.String() != strings.Repeat("0", 64) {
					result.StagedRevision = data.Signature.String()
				}
			}
		})
		if err := waitLore(ctx, op.Wait); err != nil {
			return fmt.Errorf("start Lore merge: %w", err)
		}
		if len(result.Conflicts) == 0 {
			committed, err := commitWorkspace(ctx, path, identity, message)
			if err != nil {
				return err
			}
			result.StagedRevision = committed
		}
		return nil
	})
	if err != nil {
		return MergeStartResult{}, err
	}
	return result, nil
}

func (client *SDKClient) ResolveMerge(
	ctx context.Context,
	repository RepositoryRef,
	operationID string,
	paths []string,
	strategy string,
	identity string,
) (string, error) {
	var revision string
	err := client.withWorkspace(ctx, repository, operationID, "", identity, func(path string) error {
		workspacePaths := mergeWorkspacePaths(path, paths)
		globals, cleanupGlobals := readGlobals(path, identity)
		defer cleanupGlobals()
		if strategy == "mine" {
			args, cleanup := types.NewLoreBranchMergeResolveMineArgs(
				types.LoreBranchMergeResolveMineArgs{Paths: workspacePaths},
			)
			defer cleanup()
			op := loresdk.BranchMergeResolveMine(&globals, &args)
			op.Callback(func(event *types.LoreEventFFI, _ uint64) {
				if event.Tag == types.LoreEventTag_BRANCH_MERGE_RESOLVE_REVISION {
					if data, ok := event.GetData().(*types.LoreBranchMergeResolveRevisionEventDataFFI); ok {
						revision = data.Revision.String()
					}
				}
			})
			return waitLore(ctx, op.Wait)
		}
		if strategy == "theirs" {
			args, cleanup := types.NewLoreBranchMergeResolveTheirsArgs(
				types.LoreBranchMergeResolveTheirsArgs{Paths: workspacePaths},
			)
			defer cleanup()
			op := loresdk.BranchMergeResolveTheirs(&globals, &args)
			op.Callback(func(event *types.LoreEventFFI, _ uint64) {
				if event.Tag == types.LoreEventTag_BRANCH_MERGE_RESOLVE_REVISION {
					if data, ok := event.GetData().(*types.LoreBranchMergeResolveRevisionEventDataFFI); ok {
						revision = data.Revision.String()
					}
				}
			})
			return waitLore(ctx, op.Wait)
		}
		return errors.New("merge conflict strategy must be mine or theirs")
	})
	return revision, err
}

func (client *SDKClient) ListConflicts(
	ctx context.Context,
	repository RepositoryRef,
	operationID string,
	paths []string,
	identity string,
) ([]string, error) {
	var conflicts []string
	err := client.withWorkspace(ctx, repository, operationID, "", identity, func(path string) error {
		globals, cleanupGlobals := readGlobals(path, identity)
		defer cleanupGlobals()
		args, cleanupArgs := types.NewLoreRepositoryStatusArgs(types.LoreRepositoryStatusArgs{
			Staged: true, Paths: paths,
		})
		defer cleanupArgs()
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
		return waitLore(ctx, op.Wait)
	})
	return conflicts, err
}

func (client *SDKClient) AbortMerge(
	ctx context.Context,
	repository RepositoryRef,
	operationID string,
	identity string,
) error {
	return client.withWorkspace(ctx, repository, operationID, "", identity, func(path string) error {
		globals, cleanupGlobals := readGlobals(path, identity)
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
	sourceBranch string,
	targetBranch string,
	sourceRevision string,
	targetRevision string,
	paths []string,
	identity string,
) ([]string, error) {
	err := client.withWorkspace(ctx, repository, operationID, "", identity, func(path string) error {
		globals, cleanupGlobals := readGlobals(path, identity)
		defer cleanupGlobals()
		args, cleanupArgs := types.NewLoreBranchMergeRestartArgs(types.LoreBranchMergeRestartArgs{
			Paths: mergeWorkspacePaths(path, paths),
		})
		defer cleanupArgs()
		_ = sourceBranch
		_ = sourceRevision
		if err := waitLore(ctx, loresdk.BranchMergeRestart(&globals, &args).Wait); err != nil {
			return err
		}
		_ = targetBranch
		_ = targetRevision
		return nil
	})
	if err != nil {
		return nil, err
	}
	return client.ListConflicts(ctx, repository, operationID, nil, identity)
}

func (client *SDKClient) PushMerge(
	ctx context.Context,
	repository RepositoryRef,
	operationID string,
	targetBranch string,
	identity string,
) (MergePushResult, error) {
	// StartMerge and RestartMerge leave the workspace on the requested target
	// branch. An empty Branch asks Lore to push that checked-out local branch;
	// passing the remote name can fail when an exact-revision clone has not
	// materialized a second local name for the same branch anchor.
	_ = targetBranch
	var result MergePushResult
	err := client.withWorkspace(ctx, repository, operationID, "", identity, func(path string) error {
		conflicts, err := client.listWorkspaceConflicts(ctx, path, identity)
		if err != nil {
			return err
		}
		if len(conflicts) > 0 {
			return fmt.Errorf("Lore merge still has %d unresolved conflicts", len(conflicts))
		}
		if staged, err := workspaceStagedRevision(ctx, path, identity); err != nil {
			return err
		} else if staged != "" {
			committed, err := commitWorkspace(ctx, path, identity, "Merge pull request")
			if err != nil {
				return err
			}
			result.LocalRevision = committed
		}
		globals, cleanupGlobals := readGlobals(path, identity)
		defer cleanupGlobals()
		args, cleanupArgs := types.NewLoreBranchPushArgs(types.LoreBranchPushArgs{
			Branch:           "",
			FastForwardMerge: true,
		})
		defer cleanupArgs()
		op := loresdk.BranchPush(&globals, &args)
		op.Callback(func(event *types.LoreEventFFI, _ uint64) {
			switch event.Tag {
			case types.LoreEventTag_BRANCH_PUSH:
				if data, ok := event.GetData().(*types.LoreBranchPushEventDataFFI); ok {
					result.LocalRevision = data.LocalRevision.String()
					result.RemoteRevision = data.RemoteRevision.String()
					result.AlreadyPushed = data.FlagAlreadyPushed != 0
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
		return nil
	})
	return result, err
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
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("clean Lore merge workspace: %w", err)
	}
	return nil
}

func (client *SDKClient) MergeInto(
	ctx context.Context,
	repository RepositoryRef,
	sourceBranch string,
	targetBranch string,
	message string,
	identity string,
) (MergeStartResult, error) {
	operationID := "into-" + safeOperationPart(sourceBranch) + "-" + safeOperationPart(targetBranch)
	var result MergeStartResult
	err := client.withWorkspace(ctx, repository, operationID, "", identity, func(path string) error {
		globals, cleanupGlobals := readGlobals(path, identity)
		defer cleanupGlobals()
		switchArgs, cleanupSwitch := types.NewLoreBranchSwitchArgs(types.LoreBranchSwitchArgs{Branch: sourceBranch})
		if err := waitLore(ctx, loresdk.BranchSwitch(&globals, &switchArgs).Wait); err != nil {
			cleanupSwitch()
			return err
		}
		cleanupSwitch()
		args, cleanupArgs := types.NewLoreBranchMergeIntoArgs(types.LoreBranchMergeIntoArgs{
			Branch: targetBranch, Message: message,
		})
		defer cleanupArgs()
		op := loresdk.BranchMergeInto(&globals, &args)
		op.Callback(func(event *types.LoreEventFFI, _ uint64) {
			switch event.Tag {
			case types.LoreEventTag_BRANCH_MERGE_CONFLICT_FILE:
				if data, ok := event.GetData().(*types.LoreBranchMergeConflictFileEventDataFFI); ok {
					result.Conflicts = appendUnique(result.Conflicts, data.Path.String())
				}
			case types.LoreEventTag_BRANCH_MERGE_INTO_REVISION:
				if data, ok := event.GetData().(*types.LoreBranchMergeIntoRevisionEventDataFFI); ok {
					result.StagedRevision = data.Revision.String()
				}
			}
		})
		return waitLore(ctx, op.Wait)
	})
	return result, err
}

func commitWorkspace(ctx context.Context, path string, identity string, message string) (string, error) {
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

func workspaceStagedRevision(ctx context.Context, path string, identity string) (string, error) {
	globals, cleanupGlobals := readGlobals(path, identity)
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreRepositoryStatusArgs(types.LoreRepositoryStatusArgs{Staged: true})
	defer cleanupArgs()
	var staged string
	op := loresdk.RepositoryStatus(&globals, &args)
	op.Callback(func(event *types.LoreEventFFI, _ uint64) {
		if event.Tag != types.LoreEventTag_REPOSITORY_STATUS_REVISION {
			return
		}
		if data, ok := event.GetData().(*types.LoreRepositoryStatusRevisionEventDataFFI); ok &&
			!isZeroHash(data.RevisionStaged) {
			staged = data.RevisionStaged.String()
		}
	})
	if err := waitLore(ctx, op.Wait); err != nil {
		return "", fmt.Errorf("read Lore merge status: %w", err)
	}
	return staged, nil
}

func (client *SDKClient) listWorkspaceConflicts(ctx context.Context, path string, identity string) ([]string, error) {
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
		if data, ok := event.GetData().(*types.LoreRepositoryStatusFileEventDataFFI); ok && data.FlagConflictUnresolved != 0 {
			conflicts = appendUnique(conflicts, data.Path.String())
		}
	})
	if err := waitLore(ctx, op.Wait); err != nil {
		return nil, fmt.Errorf("read Lore merge conflicts: %w", err)
	}
	return conflicts, nil
}

func sourceOrTarget(branch string) string {
	return branch
}

func mergeWorkspacePaths(workspace string, paths []string) []string {
	resolved := make([]string, 0, len(paths))
	for _, path := range paths {
		if filepath.IsAbs(path) {
			resolved = append(resolved, path)
			continue
		}
		resolved = append(resolved, filepath.Join(workspace, path))
	}
	return resolved
}

func appendUnique(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func safeOperationPart(value string) string {
	value = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, value)
	return strings.Trim(value, "-")
}
