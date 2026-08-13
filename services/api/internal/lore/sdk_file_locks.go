package lore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	loresdk "github.com/EpicGames/lore-go"
	"github.com/EpicGames/lore-go/types"
)

const maxFileLockPathBytes = 4096

func (client *SDKClient) QueryFileLocks(
	ctx context.Context,
	repository RepositoryRef,
	branch string,
	owner string,
	filePath string,
	credential Credential,
) ([]FileLock, error) {
	if err := validateFileLockRequest(repository, branch, filePath, credential, ScopeRead, true); err != nil {
		return nil, err
	}
	cachePath, err := client.prepareReadRepository(ctx, repository, credential)
	if err != nil {
		return nil, err
	}
	globals, cleanupGlobals := readGlobals(cachePath, credential.Identity)
	defer cleanupGlobals()
	queryPath := filePath
	if queryPath != "" {
		queryPath = fileLockWorkspacePath(cachePath, queryPath)
	}
	args, cleanupArgs := types.NewLoreLockFileQueryArgs(types.LoreLockFileQueryArgs{
		Branch: branch,
		Owner:  strings.TrimSpace(owner),
		Path:   queryPath,
	})
	defer cleanupArgs()
	events, err := loresdk.LockFileQuery(&globals, &args).
		FilterByType(types.LoreEventTag_LOCK_FILE_QUERY).
		Collect()
	if err != nil {
		return nil, fmt.Errorf("query Lore file locks: %w", err)
	}
	locks := make([]FileLock, 0, len(events))
	for _, event := range events {
		data, ok := event.Data.(types.LoreLockFileQueryEventData)
		if !ok || strings.TrimSpace(data.Path) == "" || strings.TrimSpace(data.Owner) == "" {
			continue
		}
		locks = append(locks, FileLock{
			BranchID: data.Branch.String(),
			Path:     data.Path,
			OwnerID:  data.Owner,
			LockedAt: time.UnixMilli(int64(data.LockedAt)).UTC(),
		})
	}
	sort.Slice(locks, func(left, right int) bool {
		if locks[left].BranchID != locks[right].BranchID {
			return locks[left].BranchID < locks[right].BranchID
		}
		return locks[left].Path < locks[right].Path
	})
	return locks, nil
}

func (client *SDKClient) AcquireFileLock(
	ctx context.Context,
	repository RepositoryRef,
	branchName string,
	filePath string,
	credential Credential,
) (FileLock, error) {
	if err := validateFileLockRequest(repository, branchName, filePath, credential, ScopeWrite, false); err != nil {
		return FileLock{}, err
	}
	branch, err := client.fileLockBranch(ctx, repository, branchName, credential)
	if err != nil {
		return FileLock{}, err
	}
	workspace, cleanup, err := client.fileLockWorkspace(ctx, repository, branch, credential)
	if err != nil {
		return FileLock{}, err
	}
	defer cleanup()
	globals, cleanupGlobals := readGlobals(workspace, credential.Identity)
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreLockFileAcquireArgs(types.LoreLockFileAcquireArgs{
		Paths: []string{fileLockWorkspacePath(workspace, filePath)}, Branch: branchName,
	})
	defer cleanupArgs()
	operationErr := waitLore(ctx, loresdk.LockFileAcquire(&globals, &args).Wait)
	lock, queryErr := client.exactFileLock(context.WithoutCancel(ctx), repository, branch, filePath, credential)
	if queryErr == nil && !credentialOwnsFileLock(credential, lock) {
		return FileLock{}, ErrFileLockConflict
	}
	if operationErr != nil {
		if queryErr == nil && lock.OwnerID == credential.Principal.UserID {
			return lock, nil
		}
		return FileLock{}, fmt.Errorf("acquire Lore file lock: %w", operationErr)
	}
	if queryErr != nil {
		return FileLock{}, fmt.Errorf("verify acquired Lore file lock: %w", queryErr)
	}
	return lock, nil
}

func (client *SDKClient) ReleaseFileLock(
	ctx context.Context,
	repository RepositoryRef,
	branchName string,
	filePath string,
	credential Credential,
	allowOwnerOverride bool,
) (FileLock, error) {
	if err := validateFileLockRequest(repository, branchName, filePath, credential, ScopeWrite, false); err != nil {
		return FileLock{}, err
	}
	branch, err := client.fileLockBranch(ctx, repository, branchName, credential)
	if err != nil {
		return FileLock{}, err
	}
	locked, err := client.exactFileLock(ctx, repository, branch, filePath, credential)
	if err != nil {
		return FileLock{}, err
	}
	if !credentialOwnsFileLock(credential, locked) && !allowOwnerOverride {
		return FileLock{}, ErrFileLockNotOwned
	}
	workspace, cleanup, err := client.fileLockWorkspace(ctx, repository, branch, credential)
	if err != nil {
		return FileLock{}, err
	}
	defer cleanup()
	current, err := client.exactFileLock(ctx, repository, branch, filePath, credential)
	if err != nil {
		return FileLock{}, err
	}
	if !sameFileLock(locked, current) {
		return FileLock{}, ErrFileLockConflict
	}
	locked = current
	// A locked file may have been removed in a later revision. Force skips the
	// workspace path check while retaining Lore Server's owner and admin checks.
	globals, cleanupGlobals := forceGlobals(workspace, credential.Identity)
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreLockFileReleaseArgs(types.LoreLockFileReleaseArgs{
		Paths: []string{fileLockWorkspacePath(workspace, filePath)}, Branch: branchName,
	})
	defer cleanupArgs()
	if err := waitLore(ctx, loresdk.LockFileRelease(&globals, &args).Wait); err != nil {
		return FileLock{}, fmt.Errorf("release Lore file lock: %w", err)
	}
	if _, err := client.exactFileLock(context.WithoutCancel(ctx), repository, branch, filePath, credential); err == nil {
		return FileLock{}, errors.New("Lore file lock release could not be verified")
	} else if !errors.Is(err, ErrFileLockNotFound) {
		return FileLock{}, fmt.Errorf("verify released Lore file lock: %w", err)
	}
	return locked, nil
}

func (client *SDKClient) exactFileLock(
	ctx context.Context,
	repository RepositoryRef,
	branch Branch,
	filePath string,
	credential Credential,
) (FileLock, error) {
	locks, err := client.QueryFileLocks(ctx, repository, branch.Name, "", filePath, credential)
	if err != nil {
		return FileLock{}, err
	}
	for _, lock := range locks {
		if lock.BranchID == branch.ID && lock.Path == filePath {
			return lock, nil
		}
	}
	return FileLock{}, ErrFileLockNotFound
}

func (client *SDKClient) fileLockBranch(
	ctx context.Context,
	repository RepositoryRef,
	branchName string,
	credential Credential,
) (Branch, error) {
	branches, err := client.Branches(ctx, repository, credential)
	if err != nil {
		return Branch{}, err
	}
	branch, found := branchByName(branches, branchName)
	if !found {
		return Branch{}, ErrBranchNotFound
	}
	return branch, nil
}

func (client *SDKClient) fileLockWorkspace(
	ctx context.Context,
	repository RepositoryRef,
	branch Branch,
	credential Credential,
) (string, func(), error) {
	workspace, err := os.MkdirTemp(client.cacheDirectory, "file-lock-")
	if err != nil {
		return "", nil, fmt.Errorf("create Lore file lock workspace: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(workspace) }
	transportURL, err := client.transportRepositoryURL(ctx, repository.URL)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	if err := client.authenticate(ctx, workspace, transportURL, credential); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := cloneBranchWorkspace(ctx, workspace, transportURL, branch.LatestRevision, credential.Identity); err != nil {
		cleanup()
		return "", nil, err
	}
	return workspace, cleanup, nil
}

func validateFileLockRequest(
	repository RepositoryRef,
	branchName string,
	filePath string,
	credential Credential,
	scope Scope,
	allowEmptyPath bool,
) error {
	if _, err := repository.ValidatedPartition(); err != nil {
		return err
	}
	if err := ValidateCredential(repository, credential, scope); err != nil {
		return err
	}
	if branchName != "" && !ValidBranchName(branchName) {
		return errors.New("Lore file lock branch is invalid")
	}
	if filePath == "" && allowEmptyPath {
		return nil
	}
	if !ValidFileLockPath(filePath) {
		return errors.New("Lore file lock path is invalid")
	}
	if strings.TrimSpace(credential.Principal.UserID) == "" && scope == ScopeWrite {
		return ErrInvalidPrincipal
	}
	return nil
}

func ValidFileLockPath(value string) bool {
	if value == "" {
		return false
	}
	if len(value) > maxFileLockPathBytes || !utf8.ValidString(value) || strings.TrimSpace(value) != value ||
		strings.Contains(value, "\\") || strings.HasPrefix(value, "/") || path.Clean(value) != value {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	for _, character := range value {
		if character == 0 || unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func forceGlobals(cachePath string, identity string) (types.LoreGlobalArgsFFI, func()) {
	return types.NewLoreGlobalArgs(types.LoreGlobalArgs{
		RepositoryPath: cachePath,
		Identity:       identity,
		Remote:         true,
		Cache:          true,
		Force:          true,
		StoreKeepAlive: true,
	})
}

func fileLockWorkspacePath(workspace string, filePath string) string {
	return filepath.Join(workspace, filepath.FromSlash(filePath))
}

func credentialOwnsFileLock(credential Credential, lock FileLock) bool {
	return credential.InsecureDevelopment || lock.OwnerID == credential.Principal.UserID
}

func sameFileLock(left FileLock, right FileLock) bool {
	return left.BranchID == right.BranchID && left.Path == right.Path &&
		left.OwnerID == right.OwnerID && left.LockedAt.Equal(right.LockedAt)
}
