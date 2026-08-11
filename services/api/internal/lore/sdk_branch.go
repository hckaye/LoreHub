package lore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	loresdk "github.com/EpicGames/lore-go"
	"github.com/EpicGames/lore-go/types"
)

func (client *SDKClient) CreateBranch(
	ctx context.Context,
	repository RepositoryRef,
	name string,
	category string,
	sourceRevision string,
	credential Credential,
) (Branch, error) {
	if err := validateBranchMutation(repository, name, category, sourceRevision, credential); err != nil {
		return Branch{}, err
	}
	branches, err := client.Branches(ctx, repository, credential)
	if err != nil {
		return Branch{}, err
	}
	if existing, found := branchByName(branches, name); found {
		if existing.LatestRevision == sourceRevision {
			return existing, nil
		}
		return Branch{}, ErrBranchExists
	}
	workspace, err := os.MkdirTemp(client.cacheDirectory, "branch-create-")
	if err != nil {
		return Branch{}, fmt.Errorf("create Lore branch workspace: %w", err)
	}
	defer func() { _ = os.RemoveAll(workspace) }()
	transportURL, err := client.transportRepositoryURL(repository.URL)
	if err != nil {
		return Branch{}, err
	}
	if err := client.authenticate(ctx, workspace, transportURL, credential); err != nil {
		return Branch{}, err
	}
	if err := cloneBranchWorkspace(ctx, workspace, transportURL, sourceRevision, credential.Identity); err != nil {
		return Branch{}, err
	}
	if err := createAndPushBranch(ctx, workspace, name, category, credential.Identity); err != nil {
		branches, listErr := client.Branches(context.WithoutCancel(ctx), repository, credential)
		if listErr == nil {
			if existing, found := branchByName(branches, name); found && existing.LatestRevision == sourceRevision {
				return existing, nil
			}
		}
		return Branch{}, err
	}
	branches, err = client.Branches(ctx, repository, credential)
	if err != nil {
		return Branch{}, err
	}
	created, found := branchByName(branches, name)
	if !found || created.LatestRevision != sourceRevision {
		return Branch{}, errors.New("Lore branch creation could not be verified")
	}
	return created, nil
}

func (client *SDKClient) ArchiveBranch(
	ctx context.Context,
	repository RepositoryRef,
	branch Branch,
	credential Credential,
) error {
	if err := validateBranchMutation(repository, branch.Name, "", branch.LatestRevision, credential); err != nil {
		return err
	}
	branches, err := client.Branches(ctx, repository, credential)
	if err != nil {
		return err
	}
	current, found := branchByName(branches, branch.Name)
	if !found {
		return nil
	}
	if current.ID != branch.ID || current.LatestRevision != branch.LatestRevision {
		return ErrBranchExists
	}
	workspace, err := os.MkdirTemp(client.cacheDirectory, "branch-archive-")
	if err != nil {
		return fmt.Errorf("create Lore branch archive workspace: %w", err)
	}
	defer func() { _ = os.RemoveAll(workspace) }()
	transportURL, err := client.transportRepositoryURL(repository.URL)
	if err != nil {
		return err
	}
	if err := client.authenticate(ctx, workspace, transportURL, credential); err != nil {
		return err
	}
	if err := cloneBranchWorkspace(ctx, workspace, transportURL, branch.LatestRevision, credential.Identity); err != nil {
		return err
	}
	globals, cleanupGlobals := readGlobals(workspace, credential.Identity)
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreBranchArchiveArgs(types.LoreBranchArchiveArgs{Branch: branch.Name})
	defer cleanupArgs()
	if err := waitLore(ctx, loresdk.BranchArchive(&globals, &args).Wait); err != nil {
		return fmt.Errorf("archive Lore branch: %w", err)
	}
	branches, err = client.Branches(ctx, repository, credential)
	if err != nil {
		return err
	}
	if _, found := branchByName(branches, branch.Name); found {
		return errors.New("Lore branch archive could not be verified")
	}
	return nil
}

func cloneBranchWorkspace(
	ctx context.Context,
	workspace string,
	repositoryURL string,
	revision string,
	identity string,
) error {
	globals, cleanupGlobals := types.NewLoreGlobalArgs(types.LoreGlobalArgs{
		RepositoryPath: workspace, Identity: identity, Remote: true, Cache: true,
	})
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreRepositoryCloneArgs(types.LoreRepositoryCloneArgs{
		RepositoryUrl: repositoryURL, Revision: revision, Bare: true,
	})
	defer cleanupArgs()
	if err := waitLore(ctx, loresdk.RepositoryClone(&globals, &args).Wait); err != nil {
		return fmt.Errorf("clone Lore branch revision: %w", err)
	}
	return nil
}

func createAndPushBranch(ctx context.Context, workspace, name, category, identity string) error {
	globals, cleanupGlobals := readGlobals(workspace, identity)
	defer cleanupGlobals()
	createArgs, cleanupCreateArgs := types.NewLoreBranchCreateArgs(types.LoreBranchCreateArgs{
		Branch: name, Category: category,
	})
	if err := waitLore(ctx, loresdk.BranchCreate(&globals, &createArgs).Wait); err != nil {
		cleanupCreateArgs()
		return fmt.Errorf("create Lore branch: %w", err)
	}
	cleanupCreateArgs()
	branchID, branchName, revision, err := workspaceBranchState(ctx, workspace, identity)
	if err != nil {
		return err
	}
	if err := validateCreatedBranchState(branchID, branchName, revision, name); err != nil {
		return err
	}
	// BranchCreate switches the current branch. Push the current anchor because
	// Lore 0.8.6 cannot resolve a newly created branch by name in this process.
	pushArgs, cleanupPushArgs := types.NewLoreBranchPushArgs(types.LoreBranchPushArgs{})
	defer cleanupPushArgs()
	if err := waitLore(ctx, loresdk.BranchPush(&globals, &pushArgs).Wait); err != nil {
		return fmt.Errorf("push Lore branch: %w", err)
	}
	return nil
}

func validateCreatedBranchState(branchID, branchName, revision, expectedName string) error {
	if branchID == "" || branchName != expectedName || isZeroRevision(revision) {
		return errors.New("Lore branch creation did not select the expected branch")
	}
	return nil
}

func validateBranchMutation(
	repository RepositoryRef,
	name string,
	category string,
	revision string,
	credential Credential,
) error {
	if _, err := repository.ValidatedPartition(); err != nil {
		return err
	}
	if err := ValidateCredential(repository, credential, ScopeWrite); err != nil {
		return err
	}
	if !ValidBranchName(name) {
		return errors.New("Lore branch name is invalid")
	}
	if category != "" && !ValidBranchCategory(category) {
		return errors.New("Lore branch category is invalid")
	}
	if len(revision) != 64 || strings.Trim(revision, "0123456789abcdef") != "" {
		return errors.New("Lore source revision is invalid")
	}
	return nil
}

func ValidBranchName(name string) bool {
	if name == "" || strings.TrimSpace(name) != name || len(name) > 255 || !utf8.ValidString(name) ||
		strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") || strings.Contains(name, "//") {
		return false
	}
	for _, segment := range strings.Split(name, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	for _, character := range name {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || strings.ContainsRune("-._/", character) {
			continue
		}
		return false
	}
	return true
}

func ValidBranchCategory(category string) bool {
	if strings.TrimSpace(category) != category || len(category) > 64 || !utf8.ValidString(category) {
		return false
	}
	for _, character := range category {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func branchByName(branches []Branch, name string) (Branch, bool) {
	for _, branch := range branches {
		if branch.Name == name && !branch.Archived {
			return branch, true
		}
	}
	return Branch{}, false
}
