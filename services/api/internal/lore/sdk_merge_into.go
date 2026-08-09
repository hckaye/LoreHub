package lore

import (
	"context"
	"errors"
	"fmt"

	loresdk "github.com/EpicGames/lore-go"
	"github.com/EpicGames/lore-go/types"
)

func (client *SDKClient) MergeInto(
	ctx context.Context,
	repository RepositoryRef,
	sourceBranch string,
	targetBranch string,
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
	sourceRevision, sourceFound := branchLatestRevision(branches, sourceBranch)
	targetRevision, targetFound := branchLatestRevision(branches, targetBranch)
	if !sourceFound || !targetFound {
		return MergeStartResult{}, ErrMergeStale
	}
	operationID := "into-" + safeOperationPart(sourceBranch) + "-" + safeOperationPart(targetBranch)
	if err := client.CleanupMergeWorkspace(ctx, repository, operationID); err != nil {
		return MergeStartResult{}, err
	}
	var result MergeStartResult
	result.SourceRevision = sourceRevision
	result.TargetRevision = targetRevision
	err = client.withWorkspace(ctx, repository, operationID, targetRevision, credential, func(path string) error {
		identity := credential.Identity
		if err := switchExactRevision(ctx, path, sourceBranch, sourceRevision, identity); err != nil {
			return err
		}
		globals, cleanupGlobals := readGlobals(path, identity)
		defer cleanupGlobals()
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
		if err := waitLore(ctx, op.Wait); err != nil {
			return err
		}
		if len(result.Conflicts) == 0 {
			parents, err := workspaceRevisionParents(ctx, path, result.StagedRevision, identity)
			if err != nil {
				return err
			}
			result.Parents = parents
			if !sameMergeParents(parents, sourceRevision, targetRevision) {
				return fmt.Errorf("%w: got %v", ErrMergeParentCheck, parents)
			}
		}
		return nil
	})
	if errors.Is(err, ErrMergeParentCheck) {
		_ = client.CleanupMergeWorkspace(context.WithoutCancel(ctx), repository, operationID)
	}
	return result, err
}
