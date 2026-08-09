package lore

import (
	"context"
	"encoding/hex"
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
	cacheDirectory           string
	allowInsecureDevelopment bool
	expectedAuthHost         string
	authLock                 sync.Mutex
	locks                    sync.Map
}

func NewSDKClient(cacheDirectory string) (*SDKClient, error) {
	return newSDKClient(cacheDirectory, false, "")
}

func NewSDKClientWithAuthAuthority(cacheDirectory string, authority string) (*SDKClient, error) {
	if err := validateAuthAuthority(authority); err != nil {
		return nil, err
	}
	return newSDKClient(cacheDirectory, false, authority)
}

func NewDevelopmentSDKClient(cacheDirectory string) (*SDKClient, error) {
	return newSDKClient(cacheDirectory, true, "")
}

func newSDKClient(cacheDirectory string, allowInsecureDevelopment bool, expectedAuthHost string) (*SDKClient, error) {
	if cacheDirectory == "" {
		return nil, errors.New("Lore cache directory is required")
	}
	if err := os.MkdirAll(cacheDirectory, 0o750); err != nil {
		return nil, fmt.Errorf("create Lore cache directory: %w", err)
	}
	return &SDKClient{
		cacheDirectory:           cacheDirectory,
		allowInsecureDevelopment: allowInsecureDevelopment,
		expectedAuthHost:         expectedAuthHost,
	}, nil
}

func (client *SDKClient) RepositoryInfo(
	ctx context.Context,
	repositoryURL string,
	credential Credential,
) (Repository, error) {
	if err := ctx.Err(); err != nil {
		return Repository{}, err
	}
	if _, err := client.validateRepositoryURL(repositoryURL); err != nil {
		return Repository{}, err
	}
	repositoryRef := RepositoryRef{URL: repositoryURL}
	if err := ValidateCredential(repositoryRef, credential, ScopeRead); err != nil {
		return Repository{}, err
	}
	if err := client.authenticate(ctx, "", repositoryURL, credential); err != nil {
		return Repository{}, err
	}
	globals, cleanupGlobals := types.NewLoreGlobalArgs(types.LoreGlobalArgs{
		Identity: credential.Identity,
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
	credential Credential,
) ([]Branch, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := client.validateRepository(repository); err != nil {
		return nil, err
	}
	if err := ValidateCredential(repository, credential, ScopeRead); err != nil {
		return nil, err
	}
	cachePath, err := client.credentialCachePath(repository, credential)
	if err != nil {
		return nil, err
	}
	lockValue, _ := client.locks.LoadOrStore(cachePath, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	if err := client.authenticate(ctx, cachePath, repository.URL, credential); err != nil {
		return nil, err
	}
	if err := client.ensureBareClone(repository.URL, cachePath, credential.Identity); err != nil {
		return nil, err
	}

	globals, cleanupGlobals := types.NewLoreGlobalArgs(types.LoreGlobalArgs{
		RepositoryPath: cachePath,
		Identity:       credential.Identity,
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

func (client *SDKClient) BranchesWithCredential(
	ctx context.Context,
	repository RepositoryRef,
	credential Credential,
) ([]Branch, error) {
	if strings.TrimSpace(credential.Token) != "" || strings.TrimSpace(credential.AuthURL) != "" {
		return nil, errors.New("Lore SDK token/AuthURL branch access requires the control-plane client adapter")
	}
	if strings.TrimSpace(credential.Identity) == "" {
		return nil, errors.New("Lore execution credential contains no supported SDK authentication material")
	}
	return client.Branches(ctx, repository, credential)
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

func (client *SDKClient) CloneRevisionWithCredential(
	ctx context.Context,
	repository RepositoryRef,
	credential Credential,
	revision string,
	destination string,
) error {
	if strings.TrimSpace(credential.Token) != "" || strings.TrimSpace(credential.AuthURL) != "" {
		return errors.New("Lore SDK token/AuthURL execution credentials require the control-plane client adapter")
	}
	if strings.TrimSpace(credential.Identity) == "" {
		return errors.New("Lore execution credential contains no supported SDK authentication material")
	}
	return client.CloneRevision(ctx, repository, credential.Identity, revision, destination)
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

func (client *SDKClient) credentialCachePath(
	repository RepositoryRef,
	credential Credential,
) (string, error) {
	base, err := client.cachePath(repository.CacheKey)
	if err != nil {
		return "", err
	}
	if !credential.Principal.valid() {
		return "", ErrInvalidPrincipal
	}
	principal := credential.Principal.UserID
	if principal == "" {
		principal = "service:" + credential.Principal.ServicePurpose + ":" + credential.Principal.Subject
	}
	key := hex.EncodeToString([]byte(principal))
	if key == "" {
		return "", ErrInvalidPrincipal
	}
	return filepath.Join(base, "principals", key), nil
}

func (client *SDKClient) authenticate(
	ctx context.Context,
	repositoryPath string,
	remoteURL string,
	credential Credential,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := client.validateRepositoryURL(remoteURL); err != nil {
		return err
	}
	if credential.InsecureDevelopment {
		if !client.allowInsecureDevelopment {
			return errors.New("insecure development Lore credential is not accepted by this client")
		}
		return nil
	}
	if client.expectedAuthHost == "" {
		return errors.New("production Lore auth authority is not configured")
	}
	if err := validateProductionCredential(credential, client.expectedAuthHost); err != nil {
		return err
	}
	lockKey := repositoryPath + "\x00" + credential.Principal.UserID + "\x00" +
		credential.Principal.ServicePurpose + "\x00" + credential.Principal.Subject
	lockValue, _ := client.locks.LoadOrStore("auth:"+lockKey, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	client.authLock.Lock()
	defer client.authLock.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	globals, cleanupGlobals := types.NewLoreGlobalArgs(types.LoreGlobalArgs{
		RepositoryPath: repositoryPath,
		Identity:       credential.Identity,
		Remote:         true,
		Cache:          repositoryPath != "",
		InMemory:       repositoryPath == "",
	})
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreAuthLoginWithTokenArgs(types.LoreAuthLoginWithTokenArgs{
		RemoteUrl: remoteURL,
		Token:     credential.Token,
		TokenType: "lore",
		AuthUrl:   credential.AuthURL,
	})
	defer cleanupArgs()
	if _, err := loresdk.AuthLoginWithToken(&globals, &args).Wait(); err != nil {
		return ErrLoreAuthentication
	}
	return nil
}

func (client *SDKClient) validateRepositoryURL(value string) (parsedRepositoryURL, error) {
	return parseRepositoryURL(value, client.allowInsecureDevelopment)
}

func (client *SDKClient) validateRepository(repository RepositoryRef) error {
	_, err := client.validateRepositoryURL(repository.URL)
	return err
}
