package lore

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	loresdk "github.com/EpicGames/lore-go"
	"github.com/EpicGames/lore-go/types"
)

const (
	maxTreeEntries   = 2_001
	maxConflictPaths = 2_001
	maxRevisionFiles = 501
)

func (client *SDKClient) Tree(
	ctx context.Context,
	repository RepositoryRef,
	revision string,
	path string,
	credential Credential,
	limit int,
) (Tree, error) {
	if err := ctx.Err(); err != nil {
		return Tree{}, err
	}
	if err := ValidateCredential(repository, credential, ScopeRead); err != nil {
		return Tree{}, err
	}
	if limit < 1 || limit >= maxTreeEntries {
		limit = maxTreeEntries - 1
	}
	session, err := client.openTree(ctx, repository, revision, credential)
	if err != nil {
		return Tree{}, err
	}
	defer session.close()

	info, err := session.info(ctx)
	if err != nil {
		return Tree{}, err
	}
	nodeID, err := session.resolve(ctx, path)
	if err != nil {
		return Tree{}, err
	}
	if _, err := session.nodeInfo(ctx, nodeID); err != nil {
		return Tree{}, err
	}
	entries, hasMore, err := session.children(ctx, nodeID, path, limit)
	if err != nil {
		return Tree{}, err
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind == "directory"
		}
		return entries[i].Name < entries[j].Name
	})
	return Tree{Revision: info.Revision.String(), Path: path, Entries: entries, HasMore: hasMore}, nil
}

func (client *SDKClient) File(
	ctx context.Context,
	repository RepositoryRef,
	revision string,
	path string,
	credential Credential,
	maxBytes int64,
) (File, []byte, error) {
	if err := ctx.Err(); err != nil {
		return File{}, nil, err
	}
	if err := ValidateCredential(repository, credential, ScopeRead); err != nil {
		return File{}, nil, err
	}
	if maxBytes < 1 {
		return File{}, nil, errors.New("file size limit must be positive")
	}
	session, err := client.openTree(ctx, repository, revision, credential)
	if err != nil {
		return File{}, nil, err
	}
	defer session.close()
	info, err := session.info(ctx)
	if err != nil {
		return File{}, nil, err
	}
	nodeID, err := session.resolve(ctx, path)
	if err != nil {
		return File{}, nil, err
	}
	node, err := session.nodeInfo(ctx, nodeID)
	if err != nil {
		return File{}, nil, err
	}
	kind := nodeKind(node.Kind)
	file := File{
		Path:     path,
		Revision: info.Revision.String(),
		Kind:     kind,
		Mode:     node.Mode,
		Size:     node.Size,
	}
	if kind != "file" {
		file.BinaryKnown = true
		return file, nil, nil
	}
	if node.Size > uint64(maxBytes) {
		file.Truncated = true
		return file, nil, nil
	}
	body, err := readAddress(ctx, session.store, session.partition, node.Address, maxBytes,
		session.cachePath, session.identity)
	if err != nil {
		return File{}, nil, err
	}
	file.BinaryKnown = true
	file.Binary = isBinary(body)
	if !file.Binary {
		file.Content = string(body)
	}
	return file, body, nil
}

func (client *SDKClient) RevisionHistory(
	ctx context.Context,
	repository RepositoryRef,
	revision string,
	branch string,
	credential Credential,
	limit int,
) ([]RevisionHistoryEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ValidateCredential(repository, credential, ScopeRead); err != nil {
		return nil, err
	}
	if limit < 1 {
		limit = 1
	}
	if limit >= maxRevisionFiles {
		limit = maxRevisionFiles - 1
	}
	cachePath, err := client.prepareReadRepository(ctx, repository, credential)
	if err != nil {
		return nil, err
	}
	entries := make([]RevisionHistoryEntry, 0, limit+1)
	identity := credential.Identity
	globals, cleanupGlobals := readGlobals(cachePath, identity)
	defer cleanupGlobals()
	historyArgs := types.LoreRevisionHistoryArgs{Revision: revision, Length: uint32(limit + 1)}
	if revision == "" {
		historyArgs.Branch = branch
		historyArgs.OnlyBranch = branch != ""
	}
	args, cleanupArgs := types.NewLoreRevisionHistoryArgs(historyArgs)
	defer cleanupArgs()
	op := loresdk.RevisionHistory(&globals, &args)
	op.Callback(func(event *types.LoreEventFFI, _ uint64) {
		if event.Tag != types.LoreEventTag_REVISION_HISTORY_ENTRY || len(entries) >= limit+1 {
			return
		}
		data, ok := event.GetData().(*types.LoreRevisionHistoryEntryEventDataFFI)
		if !ok {
			return
		}
		entries = append(entries, RevisionHistoryEntry{
			Revision: data.Revision.String(),
			Number:   data.RevisionNumber,
			Parents:  hashStrings(data.Parent[:]),
		})
	})
	if err := waitLore(ctx, op.Wait); err != nil {
		return nil, fmt.Errorf("read Lore revision history: %w", err)
	}
	return entries, nil
}

func (client *SDKClient) FileHistory(
	ctx context.Context,
	repository RepositoryRef,
	revision string,
	branch string,
	path string,
	credential Credential,
	limit int,
) ([]FileHistoryEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ValidateCredential(repository, credential, ScopeRead); err != nil {
		return nil, err
	}
	if limit < 1 {
		limit = 1
	}
	if limit >= maxRevisionFiles {
		limit = maxRevisionFiles - 1
	}
	cachePath, err := client.prepareReadRepository(ctx, repository, credential)
	if err != nil {
		return nil, err
	}
	identity := credential.Identity
	globals, cleanupGlobals := readGlobals(cachePath, identity)
	defer cleanupGlobals()
	historyArgs := types.LoreFileHistoryArgs{
		Path:     filepath.Join(cachePath, filepath.FromSlash(path)),
		Revision: revision,
		Length:   uint32(limit + 1),
		Depth:    uint32(limit + 1),
	}
	if revision == "" {
		historyArgs.Branch = branch
	}
	args, cleanupArgs := types.NewLoreFileHistoryArgs(historyArgs)
	defer cleanupArgs()
	entries := make([]FileHistoryEntry, 0, limit+1)
	op := loresdk.FileHistory(&globals, &args)
	op.Callback(func(event *types.LoreEventFFI, _ uint64) {
		if event.Tag != types.LoreEventTag_FILE_HISTORY || len(entries) >= limit+1 {
			return
		}
		data, ok := event.GetData().(*types.LoreFileHistoryEventDataFFI)
		if !ok {
			return
		}
		entries = append(entries, FileHistoryEntry{
			Path:     data.Path.String(),
			Revision: data.Revision.String(),
			Number:   data.RevisionNumber,
			Parents:  hashStrings(data.Parent[:]),
			Action:   actionName(data.Action),
			Size:     data.Size,
		})
	})
	if err := waitLore(ctx, op.Wait); err != nil {
		return nil, fmt.Errorf("read Lore file history for %q: %w", path, err)
	}
	return entries, nil
}

func (client *SDKClient) RevisionInfo(
	ctx context.Context,
	repository RepositoryRef,
	revision string,
	credential Credential,
) (Revision, error) {
	if err := ValidateCredential(repository, credential, ScopeRead); err != nil {
		return Revision{}, err
	}
	cachePath, err := client.prepareReadRepository(ctx, repository, credential)
	if err != nil {
		return Revision{}, err
	}
	var result Revision
	identity := credential.Identity
	globals, cleanupGlobals := readGlobals(cachePath, identity)
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreRevisionInfoArgs(types.LoreRevisionInfoArgs{
		Revision: revision,
		Metadata: true,
	})
	defer cleanupArgs()
	op := loresdk.RevisionInfo(&globals, &args)
	op.Callback(func(event *types.LoreEventFFI, _ uint64) {
		switch event.Tag {
		case types.LoreEventTag_REVISION_INFO:
			data, ok := event.GetData().(*types.LoreRevisionInfoEventDataFFI)
			if !ok {
				return
			}
			result.Revision = data.Revision.String()
			result.Number = data.RevisionNumber
			result.Parents = hashStrings(data.Parent[:])
		case types.LoreEventTag_METADATA:
			data, ok := event.GetData().(*types.LoreMetadataEventDataFFI)
			if ok && data.Key.String() == "message" && data.Value.Tag == types.LoreMetadataTag_STRING {
				result.Message = data.Value.AsLoreString().String()
			}
		}
	})
	if err := waitLore(ctx, op.Wait); err != nil {
		return Revision{}, fmt.Errorf("read Lore revision info: %w", err)
	}
	if result.Revision == "" {
		return Revision{}, errors.New("Lore revision response contained no revision info")
	}
	return result, nil
}

func (client *SDKClient) RevisionDiff(
	ctx context.Context,
	repository RepositoryRef,
	source string,
	target string,
	paths []string,
	credential Credential,
	maxFiles int,
	maxPatchBytes int,
) (Diff, error) {
	if err := ValidateCredential(repository, credential, ScopeRead); err != nil {
		return Diff{}, err
	}
	if maxFiles < 1 || maxFiles >= maxRevisionFiles {
		maxFiles = maxRevisionFiles - 1
	}
	if maxPatchBytes < 1 {
		return Diff{}, errors.New("diff size limit must be positive")
	}
	cachePath, err := client.prepareReadRepository(ctx, repository, credential)
	if err != nil {
		return Diff{}, err
	}
	var changed []types.LoreRevisionDiffFileEventData
	identity := credential.Identity
	globals, cleanupGlobals := readGlobals(cachePath, identity)
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreRevisionDiffArgs(types.LoreRevisionDiffArgs{
		RevisionSource: source,
		RevisionTarget: target,
		Paths:          paths,
	})
	defer cleanupArgs()
	op := loresdk.RevisionDiff(&globals, &args)
	op.Callback(func(event *types.LoreEventFFI, _ uint64) {
		if event.Tag != types.LoreEventTag_REVISION_DIFF_FILE || len(changed) >= maxFiles+1 {
			return
		}
		data, ok := event.GetData().(*types.LoreRevisionDiffFileEventDataFFI)
		if !ok {
			return
		}
		changed = append(changed, types.LoreRevisionDiffFileEventData{
			Path:       data.Path.String(),
			Action:     data.Action,
			OldIsFile:  data.OldIsFile != 0,
			NewIsFile:  data.NewIsFile != 0,
			OldAddress: data.OldAddress.Clone(),
			NewAddress: data.NewAddress.Clone(),
		})
	})
	if err := waitLore(ctx, op.Wait); err != nil {
		return Diff{}, fmt.Errorf("read Lore revision diff: %w", err)
	}
	result := Diff{Source: source, Target: target}
	if len(changed) > maxFiles {
		result.HasMore = true
		changed = changed[:maxFiles]
	}
	remaining := maxPatchBytes
	for _, item := range changed {
		if err := ctx.Err(); err != nil {
			return Diff{}, err
		}
		patch, err := client.fileDiff(ctx, cachePath, item.Path, source, target, identity, remaining)
		if err != nil {
			return Diff{}, err
		}
		if len(patch.Patch) > remaining {
			patch.Patch = patch.Patch[:remaining]
			patch.Truncated = true
		}
		remaining -= len(patch.Patch)
		if remaining == 0 {
			patch.Truncated = true
		}
		result.Truncated = result.Truncated || patch.Truncated
		result.Files = append(result.Files, patch)
	}
	return result, nil
}

func (client *SDKClient) fileDiff(
	ctx context.Context,
	cachePath string,
	path string,
	source string,
	target string,
	identity string,
	maxBytes int,
) (DiffFile, error) {
	var result = DiffFile{Path: path}
	diffPath := path
	if !filepath.IsAbs(diffPath) {
		diffPath = filepath.Join(cachePath, diffPath)
	}
	globals, cleanupGlobals := readGlobals(cachePath, identity)
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreFileDiffArgs(types.LoreFileDiffArgs{
		Paths:          []string{diffPath},
		SourceRevision: source,
		TargetRevision: target,
		ContextLines:   3,
	})
	defer cleanupArgs()
	op := loresdk.FileDiff(&globals, &args)
	op.Callback(func(event *types.LoreEventFFI, _ uint64) {
		if event.Tag != types.LoreEventTag_FILE_DIFF {
			return
		}
		data, ok := event.GetData().(*types.LoreFileDiffEventDataFFI)
		if !ok {
			return
		}
		result.Action = actionName(data.Action)
		patch := data.Patch.String()
		result.BinaryKnown = true
		result.Binary = isBinary([]byte(patch))
		if result.Binary {
			return
		}
		if len(patch) > maxBytes {
			patch = patch[:maxBytes]
			result.Truncated = true
		}
		result.Patch = patch
	})
	if err := waitLore(ctx, op.Wait); err != nil {
		return DiffFile{}, fmt.Errorf("read Lore file diff for %q: %w", path, err)
	}
	return result, nil
}

func (client *SDKClient) openTree(
	ctx context.Context,
	repository RepositoryRef,
	revision string,
	credential Credential,
) (*treeSession, error) {
	cachePath, err := client.prepareReadRepository(ctx, repository, credential)
	if err != nil {
		return nil, err
	}
	partition, err := parsePartition(repository.LoreRepositoryID)
	if err != nil {
		return nil, err
	}
	revisionHash, err := parseHash(revision)
	if err != nil {
		return nil, err
	}
	remoteURL, err := client.storageRemoteURL(repository.URL)
	if err != nil {
		return nil, err
	}
	identity := credential.Identity
	store, err := client.openStorage(ctx, cachePath, remoteURL, identity)
	if err != nil {
		return nil, err
	}
	globals, cleanupGlobals := readGlobals(cachePath, identity)
	args, cleanupArgs := types.NewLoreRevisionTreeLoadArgs(types.LoreRevisionTreeLoadArgs{
		Store:        store,
		Repository:   partition,
		RevisionHash: revisionHash,
	})
	events, callErr := loresdk.RevisionTreeLoad(&globals, &args).FilterByType(
		types.LoreEventTag_REVISION_TREE_LOADED,
	).Collect()
	cleanupArgs()
	cleanupGlobals()
	if callErr != nil {
		_ = client.closeStorage(context.WithoutCancel(ctx), store, cachePath, identity)
		return nil, fmt.Errorf("load Lore revision tree: %w", callErr)
	}
	var handleID uint64
	for _, event := range events {
		data, ok := event.Data.(types.LoreRevisionTreeLoadedEventData)
		if ok {
			handleID = data.HandleId
			break
		}
	}
	if handleID == 0 {
		_ = client.closeStorage(context.WithoutCancel(ctx), store, cachePath, identity)
		return nil, errors.New("Lore revision tree response contained no handle")
	}
	return &treeSession{
		client:    client,
		cachePath: cachePath,
		identity:  identity,
		store:     store,
		partition: partition,
		tree:      types.LoreRevisionTree{HandleId: handleID},
	}, nil
}

type treeSession struct {
	client    *SDKClient
	cachePath string
	identity  string
	store     types.LoreStore
	partition types.LorePartition
	tree      types.LoreRevisionTree
	once      sync.Once
}

func (session *treeSession) close() {
	session.once.Do(func() {
		ctx := context.Background()
		globals, cleanupGlobals := readGlobals(session.cachePath, session.identity)
		args, cleanupArgs := types.NewLoreRevisionTreeCloseArgs(types.LoreRevisionTreeCloseArgs{
			Id:     uint64(time.Now().UnixNano()),
			Handle: session.tree,
		})
		_, _ = loresdk.RevisionTreeClose(&globals, &args).Wait()
		cleanupArgs()
		cleanupGlobals()
		_ = session.client.closeStorage(ctx, session.store, session.cachePath, session.identity)
	})
}

func (session *treeSession) info(ctx context.Context) (types.LoreRevisionTreeInfoEventData, error) {
	globals, cleanupGlobals := readGlobals(session.cachePath, session.identity)
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreRevisionTreeInfoArgs(types.LoreRevisionTreeInfoArgs{
		Id:     uint64(time.Now().UnixNano()),
		Handle: session.tree,
	})
	defer cleanupArgs()
	events, err := loresdk.RevisionTreeInfo(&globals, &args).FilterByType(
		types.LoreEventTag_REVISION_TREE_INFO,
	).Collect()
	if err != nil {
		return types.LoreRevisionTreeInfoEventData{}, fmt.Errorf("read Lore tree info: %w", err)
	}
	for _, event := range events {
		if data, ok := event.Data.(types.LoreRevisionTreeInfoEventData); ok {
			if data.ErrorCode != types.LoreErrorCode_NONE {
				return types.LoreRevisionTreeInfoEventData{}, fmt.Errorf("Lore tree info failed with code %d", data.ErrorCode)
			}
			return data, nil
		}
	}
	return types.LoreRevisionTreeInfoEventData{}, errors.New("Lore tree info response was empty")
}

func (session *treeSession) resolve(ctx context.Context, path string) (uint32, error) {
	globals, cleanupGlobals := readGlobals(session.cachePath, session.identity)
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreRevisionTreeResolvePathArgs(types.LoreRevisionTreeResolvePathArgs{
		Id:     uint64(time.Now().UnixNano()),
		Handle: session.tree,
		Path:   path,
	})
	defer cleanupArgs()
	events, err := loresdk.RevisionTreeResolvePath(&globals, &args).FilterByType(
		types.LoreEventTag_REVISION_TREE_RESOLVE_PATH_COMPLETE,
	).Collect()
	if err != nil {
		return 0, fmt.Errorf("resolve Lore tree path: %w", err)
	}
	for _, event := range events {
		if data, ok := event.Data.(types.LoreRevisionTreeResolvePathCompleteEventData); ok {
			if data.ErrorCode != types.LoreErrorCode_NONE {
				return 0, fmt.Errorf("%w: Lore tree path code %d", ErrNotFound, data.ErrorCode)
			}
			return data.NodeId, nil
		}
	}
	return 0, errors.New("Lore tree path response was empty")
}

func (session *treeSession) nodeInfo(
	ctx context.Context,
	nodeID uint32,
) (types.LoreRevisionTreeNodeInfoEventData, error) {
	globals, cleanupGlobals := readGlobals(session.cachePath, session.identity)
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreRevisionTreeNodeInfoArgs(types.LoreRevisionTreeNodeInfoArgs{
		Id:     uint64(time.Now().UnixNano()),
		Handle: session.tree,
		NodeId: nodeID,
	})
	defer cleanupArgs()
	events, err := loresdk.RevisionTreeNodeInfo(&globals, &args).FilterByType(
		types.LoreEventTag_REVISION_TREE_NODE_INFO,
	).Collect()
	if err != nil {
		return types.LoreRevisionTreeNodeInfoEventData{}, fmt.Errorf("read Lore tree node: %w", err)
	}
	for _, event := range events {
		if data, ok := event.Data.(types.LoreRevisionTreeNodeInfoEventData); ok {
			if data.ErrorCode != types.LoreErrorCode_NONE {
				return types.LoreRevisionTreeNodeInfoEventData{}, fmt.Errorf("Lore tree node failed with code %d", data.ErrorCode)
			}
			return data, nil
		}
	}
	return types.LoreRevisionTreeNodeInfoEventData{}, errors.New("Lore tree node response was empty")
}

func (session *treeSession) children(
	ctx context.Context,
	parentID uint32,
	parentPath string,
	limit int,
) ([]TreeEntry, bool, error) {
	entries := make([]TreeEntry, 0, limit)
	globals, cleanupGlobals := readGlobals(session.cachePath, session.identity)
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreRevisionTreeListChildrenArgs(types.LoreRevisionTreeListChildrenArgs{
		Id:           uint64(time.Now().UnixNano()),
		Handle:       session.tree,
		ParentNodeId: parentID,
	})
	defer cleanupArgs()
	op := loresdk.RevisionTreeListChildren(&globals, &args)
	hasMore := false
	op.Callback(func(event *types.LoreEventFFI, _ uint64) {
		if event.Tag != types.LoreEventTag_REVISION_TREE_CHILD {
			return
		}
		data, ok := event.GetData().(*types.LoreRevisionTreeChildEventDataFFI)
		if !ok {
			return
		}
		if len(entries) >= limit {
			hasMore = true
			return
		}
		name := data.Name.String()
		entries = append(entries, TreeEntry{
			Name: name,
			Path: joinLorePath(parentPath, name),
			Kind: nodeKind(data.Kind),
			Mode: data.Mode,
			Size: data.Size,
		})
	})
	if err := waitLore(ctx, op.Wait); err != nil {
		return nil, false, fmt.Errorf("list Lore tree children: %w", err)
	}
	return entries, hasMore, nil
}

func (client *SDKClient) openStorage(
	ctx context.Context,
	cachePath string,
	remoteURL string,
	identity string,
) (types.LoreStore, error) {
	globals, cleanupGlobals := readGlobals(cachePath, identity)
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreStorageOpenArgs(types.LoreStorageOpenArgs{
		RepositoryPath:  cachePath,
		RemoteConfig:    types.LoreStorageRemoteConfig{RemoteUrl: remoteURL},
		HasRemoteConfig: true,
	})
	defer cleanupArgs()
	events, err := loresdk.StorageOpen(&globals, &args).FilterByType(
		types.LoreEventTag_STORAGE_OPENED,
	).Collect()
	if err != nil {
		return types.LoreStore{}, fmt.Errorf("open Lore storage: %w", err)
	}
	for _, event := range events {
		if data, ok := event.Data.(types.LoreStorageOpenedEventData); ok {
			return types.LoreStore{HandleId: data.HandleId}, nil
		}
	}
	return types.LoreStore{}, errors.New("Lore storage response contained no handle")
}

func (client *SDKClient) storageRemoteURL(repositoryURL string) (string, error) {
	parsed, err := client.validateRepositoryURL(repositoryURL)
	if err != nil {
		return "", err
	}
	return parsed.Scheme + "://" + parsed.Authority, nil
}

func (client *SDKClient) closeStorage(
	ctx context.Context,
	store types.LoreStore,
	cachePath string,
	identity string,
) error {
	if store.HandleId == 0 {
		return nil
	}
	globals, cleanupGlobals := readGlobals(cachePath, identity)
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreStorageCloseArgs(types.LoreStorageCloseArgs{Handle: store})
	defer cleanupArgs()
	return waitLore(ctx, loresdk.StorageClose(&globals, &args).Wait)
}

func readAddress(
	ctx context.Context,
	store types.LoreStore,
	partition types.LorePartition,
	address types.LoreAddress,
	maxBytes int64,
	cachePath string,
	identity string,
) ([]byte, error) {
	globals, cleanupGlobals := readGlobals(cachePath, identity)
	defer cleanupGlobals()
	args, cleanupArgs := types.NewLoreStorageGetArgs(types.LoreStorageGetArgs{
		Handle: store,
		Items: []types.LoreStorageGetItem{{
			Id:         1,
			Partition:  partition,
			Address:    address,
			Streaming:  true,
			LocalCache: true,
		}},
	})
	defer cleanupArgs()
	var body []byte
	var expected uint64
	op := loresdk.StorageGet(&globals, &args)
	op.Callback(func(event *types.LoreEventFFI, _ uint64) {
		switch event.Tag {
		case types.LoreEventTag_STORAGE_GET_HEADER:
			if data, ok := event.GetData().(*types.LoreStorageGetHeaderEventDataFFI); ok {
				expected = data.SizeContent
			}
		case types.LoreEventTag_STORAGE_GET_DATA:
			data, ok := event.GetData().(*types.LoreStorageGetDataEventDataFFI)
			if !ok || int64(len(body)) >= maxBytes {
				return
			}
			part := data.Bytes.Clone()
			remaining := int(maxBytes - int64(len(body)))
			if len(part) > remaining {
				part = part[:remaining]
			}
			body = append(body, part...)
		}
	})
	if err := waitLore(ctx, op.Wait); err != nil {
		return nil, fmt.Errorf("read Lore file content: %w", err)
	}
	if expected > uint64(maxBytes) || int64(len(body)) > maxBytes {
		return nil, errors.New("Lore file content exceeded the configured limit")
	}
	return body, nil
}

func (client *SDKClient) prepareReadRepository(
	ctx context.Context,
	repository RepositoryRef,
	credential Credential,
) (string, error) {
	cachePath, err := client.credentialCachePath(repository, credential)
	if err != nil {
		return "", err
	}
	lockValue, _ := client.locks.LoadOrStore(cachePath, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	if err := client.authenticate(ctx, cachePath, repository.URL, credential); err != nil {
		return "", err
	}
	if err := client.ensureBareClone(repository.URL, cachePath, credential.Identity); err != nil {
		return "", err
	}
	return cachePath, nil
}

func readGlobals(cachePath string, identity string) (types.LoreGlobalArgsFFI, func()) {
	return types.NewLoreGlobalArgs(types.LoreGlobalArgs{
		RepositoryPath: cachePath,
		Identity:       identity,
		Remote:         true,
		Cache:          true,
		StoreKeepAlive: true,
	})
}

func waitLore(ctx context.Context, wait func() (int32, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := wait()
	if err != nil {
		return err
	}
	return ctx.Err()
}

func parseHash(value string) (types.LoreHash, error) {
	var result types.LoreHash
	decoded, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil || len(decoded) != len(result.Data) {
		return result, errors.New("Lore revision must be a 64-character hexadecimal value")
	}
	copy(result.Data[:], decoded)
	return result, nil
}

func parsePartition(value string) (types.LorePartition, error) {
	var result types.LorePartition
	decoded, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil || len(decoded) != len(result.Data) {
		return result, errors.New("Lore repository ID must be a 32-character hexadecimal value")
	}
	copy(result.Data[:], decoded)
	return result, nil
}

func hashStrings(values []types.LoreHash) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if isZeroHash(value) {
			continue
		}
		result = append(result, value.String())
	}
	return result
}

func isZeroHash(value types.LoreHash) bool {
	return bytes.Equal(value.Data[:], make([]byte, len(value.Data)))
}

func nodeKind(kind uint32) string {
	switch types.LoreNodeType(kind) {
	case types.LoreNodeType_DIRECTORY:
		return "directory"
	case types.LoreNodeType_LINK:
		return "link"
	default:
		return "file"
	}
}

func actionName(action types.LoreFileAction) string {
	switch action {
	case types.LoreFileAction_ADD:
		return "added"
	case types.LoreFileAction_DELETE:
		return "deleted"
	case types.LoreFileAction_MOVE:
		return "moved"
	case types.LoreFileAction_COPY:
		return "copied"
	default:
		return "modified"
	}
}

func joinLorePath(parent string, name string) string {
	if parent == "" {
		return name
	}
	return filepath.ToSlash(filepath.Join(parent, name))
}

func isBinary(body []byte) bool {
	return bytes.IndexByte(body, 0) >= 0 || !utf8.Valid(body)
}
