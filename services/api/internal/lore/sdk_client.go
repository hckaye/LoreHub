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

type SDKClient struct {
	cacheDirectory string
	locks          sync.Map
}

func NewSDKClient(cacheDirectory string) (*SDKClient, error) {
	if cacheDirectory == "" {
		return nil, errors.New("Lore cache directory is required")
	}
	if err := os.MkdirAll(cacheDirectory, 0o750); err != nil {
		return nil, fmt.Errorf("create Lore cache directory: %w", err)
	}
	return &SDKClient{cacheDirectory: cacheDirectory}, nil
}

func (client *SDKClient) RepositoryInfo(
	ctx context.Context,
	repositoryURL string,
	identity string,
) (Repository, error) {
	if err := ctx.Err(); err != nil {
		return Repository{}, err
	}
	if !strings.HasPrefix(repositoryURL, "lore://") {
		return Repository{}, errors.New("repository URL must use the lore scheme")
	}
	globals, cleanupGlobals := types.NewLoreGlobalArgs(types.LoreGlobalArgs{
		Identity: identity,
		Remote:   true,
		InMemory: true,
	})
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreRepositoryInfoArgs(types.LoreRepositoryInfoArgs{
		RepositoryUrl: repositoryURL,
	})
	defer cleanupArgs()

	events, err := loresdk.RepositoryInfo(&globals, &args).
		FilterByType(types.LoreEventTag_REPOSITORY_DATA).
		Collect()
	if err != nil {
		return Repository{}, fmt.Errorf("query Lore repository: %w", err)
	}
	for _, event := range events {
		data, ok := event.Data.(types.LoreRepositoryDataEventData)
		if !ok {
			continue
		}
		return Repository{
			ID:            data.Id.String(),
			Name:          data.Name,
			Description:   data.Description,
			URL:           data.RemoteUrl,
			DefaultBranch: data.DefaultBranchName,
			Creator:       data.Creator,
			CreatedAt:     time.Unix(int64(data.Created), 0).UTC(),
		}, nil
	}
	return Repository{}, errors.New("Lore repository response contained no repository data")
}

func (client *SDKClient) Branches(
	ctx context.Context,
	repository RepositoryRef,
	identity string,
) ([]Branch, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cachePath, err := client.cachePath(repository.CacheKey)
	if err != nil {
		return nil, err
	}
	lockValue, _ := client.locks.LoadOrStore(cachePath, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	if err := client.ensureBareClone(repository.URL, cachePath, identity); err != nil {
		return nil, err
	}

	globals, cleanupGlobals := types.NewLoreGlobalArgs(types.LoreGlobalArgs{
		RepositoryPath: cachePath,
		Identity:       identity,
		Remote:         true,
		StoreKeepAlive: true,
	})
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreBranchListArgs(types.LoreBranchListArgs{})
	defer cleanupArgs()
	events, err := loresdk.BranchList(&globals, &args).
		FilterByType(types.LoreEventTag_BRANCH_LIST_ENTRY).
		Collect()
	if err != nil {
		return nil, fmt.Errorf("list Lore branches: %w", err)
	}

	branches := make([]Branch, 0, len(events))
	for _, event := range events {
		data, ok := event.Data.(types.LoreBranchListEntryEventData)
		if !ok {
			continue
		}
		branches = append(branches, Branch{
			ID:             data.Id.String(),
			Name:           data.Name,
			Category:       data.Category,
			LatestRevision: data.Latest.String(),
			Creator:        data.Creator,
			CreatedAt:      time.Unix(int64(data.Created), 0).UTC(),
			Current:        data.IsCurrent,
			Archived:       data.Archived,
		})
	}
	return branches, nil
}

func (client *SDKClient) CloneRevision(
	ctx context.Context,
	repository RepositoryRef,
	identity string,
	revision string,
	destination string,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if revision == "" || destination == "" {
		return errors.New("Lore revision and destination are required")
	}
	if filepath.IsAbs(destination) == false {
		return errors.New("Lore revision destination must be absolute")
	}
	if err := os.MkdirAll(destination, 0o750); err != nil {
		return fmt.Errorf("create Lore revision workspace: %w", err)
	}
	globals, cleanupGlobals := types.NewLoreGlobalArgs(types.LoreGlobalArgs{
		RepositoryPath: destination,
		Identity:       identity,
		Remote:         true,
	})
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreRepositoryCloneArgs(types.LoreRepositoryCloneArgs{
		RepositoryUrl: repository.URL,
		Revision:      revision,
	})
	defer cleanupArgs()
	if _, err := loresdk.RepositoryClone(&globals, &args).Wait(); err != nil {
		return fmt.Errorf("clone Lore revision %s: %w", revision, err)
	}
	return nil
}

func (client *SDKClient) ensureBareClone(repositoryURL string, cachePath string, identity string) error {
	if _, err := os.Stat(filepath.Join(cachePath, ".lore", "config.toml")); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect Lore repository cache: %w", err)
	}
	if err := os.MkdirAll(cachePath, 0o750); err != nil {
		return fmt.Errorf("create Lore repository cache: %w", err)
	}

	globals, cleanupGlobals := types.NewLoreGlobalArgs(types.LoreGlobalArgs{
		RepositoryPath: cachePath,
		Identity:       identity,
		Cache:          true,
	})
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreRepositoryCloneArgs(types.LoreRepositoryCloneArgs{
		RepositoryUrl: repositoryURL,
		Bare:          true,
	})
	defer cleanupArgs()
	if _, err := loresdk.RepositoryClone(&globals, &args).Wait(); err != nil {
		return fmt.Errorf("create bare Lore repository cache: %w", err)
	}
	return nil
}

func (client *SDKClient) cachePath(key string) (string, error) {
	if key == "" || strings.ContainsAny(key, `/\\`) || key == "." || key == ".." {
		return "", errors.New("invalid Lore cache key")
	}
	path := filepath.Join(client.cacheDirectory, "repositories", key)
	if !strings.HasPrefix(path, client.cacheDirectory+string(filepath.Separator)) {
		return "", errors.New("Lore cache path escapes cache directory")
	}
	return path, nil
}
