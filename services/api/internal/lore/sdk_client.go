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
	dataPlaneOrigin          string
	authLock                 sync.Mutex
	locks                    sync.Map
}

func NewSDKClient(cacheDirectory string) (*SDKClient, error) {
	return newSDKClient(cacheDirectory, false, "", "")
}

func NewSDKClientWithAuthAuthority(cacheDirectory string, authority string) (*SDKClient, error) {
	return NewSDKClientWithEndpoints(cacheDirectory, authority, "")
}

func NewSDKClientWithEndpoints(
	cacheDirectory string,
	authority string,
	dataPlaneOrigin string,
) (*SDKClient, error) {
	if err := validateAuthAuthority(authority); err != nil {
		return nil, err
	}
	return newSDKClient(cacheDirectory, false, authority, dataPlaneOrigin)
}

func NewDevelopmentSDKClient(cacheDirectory string) (*SDKClient, error) {
	return newSDKClient(cacheDirectory, true, "", "")
}

func NewDevelopmentSDKClientWithEndpoint(
	cacheDirectory string,
	dataPlaneOrigin string,
) (*SDKClient, error) {
	return newSDKClient(cacheDirectory, true, "", dataPlaneOrigin)
}

func newSDKClient(
	cacheDirectory string,
	allowInsecureDevelopment bool,
	expectedAuthHost string,
	dataPlaneOrigin string,
) (*SDKClient, error) {
	if cacheDirectory == "" {
		return nil, errors.New("Lore cache directory is required")
	}
	normalizedOrigin, err := normalizeDataPlaneOrigin(dataPlaneOrigin, allowInsecureDevelopment)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cacheDirectory, 0o750); err != nil {
		return nil, fmt.Errorf("create Lore cache directory: %w", err)
	}
	return &SDKClient{
		cacheDirectory:           cacheDirectory,
		allowInsecureDevelopment: allowInsecureDevelopment,
		expectedAuthHost:         expectedAuthHost,
		dataPlaneOrigin:          normalizedOrigin,
	}, nil
}

func normalizeDataPlaneOrigin(value string, allowPlain bool) (string, error) {
	if value == "" {
		return "", nil
	}
	if strings.HasSuffix(value, "/") {
		return "", errors.New("Lore data-plane origin must not end with a slash")
	}
	const validationPartition = "00000000000000000000000000000000"
	parsed, err := parseRepositoryURL(value+"/"+validationPartition, allowPlain)
	if err != nil || parsed.Partition != validationPartition {
		return "", errors.New("Lore data-plane origin must be a fixed Lore authority")
	}
	return parsed.Scheme + "://" + parsed.Authority, nil
}

func (client *SDKClient) transportRepositoryURL(repositoryURL string) (string, error) {
	parsed, err := client.validateRepositoryURL(repositoryURL)
	if err != nil {
		return "", err
	}
	if client.dataPlaneOrigin == "" {
		return repositoryURL, nil
	}
	return client.dataPlaneOrigin + "/" + parsed.Partition, nil
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
	workspace, err := os.MkdirTemp(client.cacheDirectory, "info-"+credential.Partition+"-")
	if err != nil {
		return Repository{}, fmt.Errorf("create Lore information workspace: %w", err)
	}
	defer func() { _ = os.RemoveAll(workspace) }()
	if err := client.authenticate(ctx, workspace, repositoryURL, credential); err != nil {
		return Repository{}, err
	}
	globals, cleanupGlobals := types.NewLoreGlobalArgs(types.LoreGlobalArgs{
		RepositoryPath: workspace,
		Identity:       credential.Identity,
		Remote:         true,
		InMemory:       true,
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
	if err := client.ensureBareClone(
		repository.URL,
		cachePath,
		credential.Identity,
		credential.Partition,
	); err != nil {
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

func (client *SDKClient) CloneWithCredential(
	ctx context.Context,
	repositoryURL string,
	revision string,
	destination string,
	credential Credential,
) error {
	parsed, err := client.validateRepositoryURL(repositoryURL)
	if err != nil {
		return err
	}
	ref := RepositoryRef{URL: repositoryURL, LoreRepositoryID: parsed.Partition}
	if err := ValidateCredential(ref, credential, ScopeRead); err != nil {
		return err
	}
	transportURL, err := client.transportRepositoryURL(repositoryURL)
	if err != nil {
		return err
	}
	if strings.TrimSpace(destination) == "" || strings.ContainsAny(destination, "\x00\r\n") {
		return errors.New("Lore clone destination is invalid")
	}
	if err := client.authenticate(ctx, destination, transportURL, credential); err != nil {
		return err
	}
	globals, cleanupGlobals := types.NewLoreGlobalArgs(types.LoreGlobalArgs{
		RepositoryPath: destination, Identity: credential.Identity, Remote: true, Cache: true,
	})
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreRepositoryCloneArgs(types.LoreRepositoryCloneArgs{
		RepositoryUrl: transportURL, Revision: revision,
	})
	defer cleanupArgs()
	if _, err := loresdk.RepositoryClone(&globals, &args).Wait(); err != nil {
		return fmt.Errorf("clone Lore revision: %w", err)
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
	if _, err := repository.ValidatedPartition(); err != nil {
		return err
	}
	return client.CloneWithCredential(ctx, repository.URL, revision, destination, credential)
}

func (client *SDKClient) CreateRepositoryWithCredential(
	ctx context.Context,
	repositoryURL string,
	repositoryID string,
	name string,
	description string,
	credential Credential,
) error {
	if len(repositoryID) != 32 {
		return errors.New("Lore repository ID must be exactly 32 hexadecimal characters")
	}
	if _, err := hex.DecodeString(repositoryID); err != nil {
		return errors.New("Lore repository ID must be exactly 32 hexadecimal characters")
	}
	parsed, err := client.validateRepositoryURL(repositoryURL)
	if err != nil {
		return err
	}
	if parsed.Partition != repositoryID {
		return errors.New("Lore repository URL partition does not match repository ID")
	}
	if err := ValidateCredential(RepositoryRef{URL: repositoryURL, LoreRepositoryID: repositoryID}, credential,
		ScopeWrite); err != nil {
		return err
	}
	workspace, err := os.MkdirTemp(client.cacheDirectory, "provision-"+repositoryID+"-")
	if err != nil {
		return fmt.Errorf("create Lore provisioning workspace: %w", err)
	}
	defer func() { _ = os.RemoveAll(workspace) }()
	if err := client.authenticateResourceToken(ctx, workspace, repositoryURL, credential); err != nil {
		return err
	}
	globals, cleanupGlobals := types.NewLoreGlobalArgs(types.LoreGlobalArgs{
		RepositoryPath: workspace, Identity: credential.Identity, Remote: true, InMemory: true,
	})
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreRepositoryCreateArgs(types.LoreRepositoryCreateArgs{
		RepositoryUrl: repositoryURL, Description: description, Id: repositoryID, UseSharedStore: true,
	})
	defer cleanupArgs()
	if _, err := loresdk.RepositoryCreate(&globals, &args).Wait(); err != nil {
		return fmt.Errorf("create Lore repository %q: %w", name, err)
	}
	return nil
}

func (client *SDKClient) ensureBareClone(
	repositoryURL string,
	cachePath string,
	identity string,
	repositoryID string,
) error {
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
	if _, cloneErr := loresdk.RepositoryClone(&globals, &args).Wait(); cloneErr != nil {
		if !isEmptyRemoteCloneError(cloneErr) {
			return fmt.Errorf("create bare Lore repository cache: %w", cloneErr)
		}
		if initErr := initializeEmptyRepositoryCache(
			repositoryURL,
			cachePath,
			identity,
			repositoryID,
		); initErr != nil {
			return errors.Join(fmt.Errorf("clone empty Lore repository: %w", cloneErr), initErr)
		}
	}
	return nil
}

func isEmptyRemoteCloneError(err error) bool {
	var loreErr *loresdk.LoreError
	if !errors.As(err, &loreErr) || loreErr.ReturnCode != 13 {
		return false
	}
	for _, message := range loreErr.Messages {
		if strings.TrimSpace(message) == "Not found" {
			return true
		}
	}
	return false
}

func initializeEmptyRepositoryCache(
	repositoryURL string,
	cachePath string,
	identity string,
	repositoryID string,
) error {
	if len(repositoryID) != 32 {
		return errors.New("empty Lore repository cache requires the exact repository ID")
	}
	globals, cleanupGlobals := types.NewLoreGlobalArgs(types.LoreGlobalArgs{
		RepositoryPath: cachePath,
		Identity:       identity,
		Offline:        true,
	})
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreRepositoryCreateArgs(types.LoreRepositoryCreateArgs{
		RepositoryUrl: repositoryURL,
		Id:            repositoryID,
	})
	defer cleanupArgs()
	if _, err := loresdk.RepositoryCreate(&globals, &args).Wait(); err != nil {
		return fmt.Errorf("initialize empty Lore repository cache: %w", err)
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
	return client.authenticateWithToken(ctx, repositoryPath, remoteURL, credential,
		credential.AuthenticationToken, AuthenticationTokenType)
}

// Repository creation connects before the partition exists, so Lore cannot
// perform its normal base-token exchange yet. The already-scoped token is used
// only for this genesis operation and is still verified by Lore Server.
func (client *SDKClient) authenticateResourceToken(
	ctx context.Context,
	repositoryPath string,
	remoteURL string,
	credential Credential,
) error {
	return client.authenticateWithToken(ctx, repositoryPath, remoteURL, credential, credential.Token, "lore")
}

func (client *SDKClient) authenticateWithToken(
	ctx context.Context,
	repositoryPath string,
	remoteURL string,
	credential Credential,
	token string,
	tokenType string,
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
		Token:     token,
		TokenType: tokenType,
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
	parsed, err := client.validateRepositoryURL(repository.URL)
	if err != nil {
		return err
	}
	idPartition := strings.TrimSpace(repository.LoreRepositoryID)
	if idPartition != "" && idPartition != parsed.Partition {
		return errors.New("Lore repository URL partition does not match repository ID")
	}
	return nil
}
