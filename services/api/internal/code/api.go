// Package code exposes Lore-native repository browsing and revision APIs.
package code

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/lorehub/lorehub/services/api/internal/collab"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type API struct {
	store               collab.Store
	lore                loreclient.Client
	code                loreclient.CodeClient
	actors              collab.ActorResolver
	users               UserLookup
	credentials         loreclient.CredentialProvider
	publicReaderSubject string
	logger              *slog.Logger
}

type UserLookup interface {
	ActiveUser(context.Context, string) (platform.User, error)
}

// Register mounts read-only repository browsing endpoints. Authentication is
// resolved for every request so private repositories use the same visibility
// rules as collaboration APIs.
func Register(
	mux *http.ServeMux,
	store collab.Store,
	lore loreclient.Client,
	codeClient loreclient.CodeClient,
	actors collab.ActorResolver,
	users UserLookup,
	credentials loreclient.CredentialProvider,
	publicReaderSubject string,
	logger *slog.Logger,
) {
	api := &API{
		store: store, lore: lore, code: codeClient, actors: actors, users: users,
		credentials: credentials, logger: logger,
	}
	api.publicReaderSubject = publicReaderSubject
	base := "/api/v1/repositories/{owner}/{repository}"
	mux.HandleFunc("GET "+base+"/tree", api.tree)
	mux.HandleFunc("GET "+base+"/file", api.file)
	mux.HandleFunc("GET "+base+"/file/history", api.fileHistory)
	mux.HandleFunc("GET "+base+"/raw", api.raw)
	mux.HandleFunc("GET "+base+"/revisions", api.history)
	mux.HandleFunc("GET "+base+"/revisions/{revision}", api.revision)
	mux.HandleFunc("GET "+base+"/diff", api.diff)
}

func (api *API) visibleRepository(
	writer http.ResponseWriter,
	request *http.Request,
) (collab.Repository, *platform.User, bool) {
	actor, ok := api.actors.ResolveOptionalActor(writer, request)
	if !ok {
		return collab.Repository{}, nil, false
	}
	repository, err := api.store.LookupRepository(request.Context(), actor,
		request.PathValue("owner"), request.PathValue("repository"))
	if err != nil {
		api.storeError(writer, request, "lookup repository", err)
		return collab.Repository{}, nil, false
	}
	if repository.LoreURL == "" || repository.LoreRepositoryID == "" {
		api.problem(writer, http.StatusBadGateway, "lore_unavailable", "The Lore repository is not configured")
		return collab.Repository{}, nil, false
	}
	return repository, actor, true
}

func (api *API) tree(writer http.ResponseWriter, request *http.Request) {
	repository, actor, ok := api.visibleRepository(writer, request)
	if !ok {
		return
	}
	revision, ok := api.resolveRevision(writer, request, repository, actor)
	if !ok {
		return
	}
	path, err := normalizePath(request.URL.Query().Get("path"))
	if err != nil {
		api.problem(writer, http.StatusBadRequest, "invalid_path", "The repository path is invalid")
		return
	}
	if api.code == nil {
		api.problem(writer, http.StatusServiceUnavailable, "lore_unavailable", "Lore code browsing is unavailable")
		return
	}
	credential, err := api.credential(request, repository, actor, loreclient.ScopeRead)
	if err != nil {
		api.loreError(writer, request, "get Lore read credential", err)
		return
	}
	result, err := api.code.Tree(request.Context(), api.repositoryRef(repository), revision, path, credential,
		boundedInt(request.URL.Query().Get("limit"), 100, maxTreeLimit))
	if err != nil {
		api.loreError(writer, request, "list Lore tree", err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (api *API) file(writer http.ResponseWriter, request *http.Request) {
	repository, actor, ok := api.visibleRepository(writer, request)
	if !ok {
		return
	}
	revision, ok := api.resolveRevision(writer, request, repository, actor)
	if !ok {
		return
	}
	path, err := normalizePath(request.URL.Query().Get("path"))
	if err != nil || path == "" {
		api.problem(writer, http.StatusBadRequest, "invalid_path", "A file path is required")
		return
	}
	if api.code == nil {
		api.problem(writer, http.StatusServiceUnavailable, "lore_unavailable", "Lore code browsing is unavailable")
		return
	}
	credential, err := api.credential(request, repository, actor, loreclient.ScopeRead)
	if err != nil {
		api.loreError(writer, request, "get Lore read credential", err)
		return
	}
	result, _, err := api.code.File(
		request.Context(), api.repositoryRef(repository), revision, path, credential, maxFileBytes,
	)
	if err != nil {
		api.loreError(writer, request, "read Lore file", err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (api *API) fileHistory(writer http.ResponseWriter, request *http.Request) {
	repository, actor, ok := api.visibleRepository(writer, request)
	if !ok {
		return
	}
	revision, ok := api.resolveRevision(writer, request, repository, actor)
	if !ok {
		return
	}
	path, err := normalizePath(request.URL.Query().Get("path"))
	if err != nil || path == "" {
		api.problem(writer, http.StatusBadRequest, "invalid_path", "A file path is required")
		return
	}
	branch, err := optionalBranch(request.URL.Query().Get("branch"))
	if err != nil {
		api.problem(writer, http.StatusBadRequest, "invalid_branch", "The Lore branch is invalid")
		return
	}
	if branch == "" && request.URL.Query().Get("revision") == "" {
		branch, err = normalizeBranch(repository.DefaultBranch)
		if err != nil {
			api.problem(writer, http.StatusBadRequest, "invalid_branch", "The Lore branch is invalid")
			return
		}
	}
	if api.code == nil {
		api.problem(writer, http.StatusServiceUnavailable, "lore_unavailable", "Lore file history is unavailable")
		return
	}
	credential, err := api.credential(request, repository, actor, loreclient.ScopeRead)
	if err != nil {
		api.loreError(writer, request, "get Lore read credential", err)
		return
	}
	entries, err := api.code.FileHistory(
		request.Context(), api.repositoryRef(repository), historyRevision(request, revision), branch, path,
		credential, boundedInt(request.URL.Query().Get("limit"), 50, maxHistoryLimit),
	)
	if err != nil {
		api.loreError(writer, request, "read Lore file history", err)
		return
	}
	entries, hasMore := historyWindow(entries, boundedInt(request.URL.Query().Get("limit"), 50, maxHistoryLimit))
	writeJSON(writer, http.StatusOK, map[string]any{"revision": revision, "path": path, "entries": entries,
		"hasMore": hasMore})
}

func (api *API) raw(writer http.ResponseWriter, request *http.Request) {
	repository, actor, ok := api.visibleRepository(writer, request)
	if !ok {
		return
	}
	revision, ok := api.resolveRevision(writer, request, repository, actor)
	if !ok {
		return
	}
	path, err := normalizePath(request.URL.Query().Get("path"))
	if err != nil || path == "" {
		api.problem(writer, http.StatusBadRequest, "invalid_path", "A file path is required")
		return
	}
	if api.code == nil {
		api.problem(writer, http.StatusServiceUnavailable, "lore_unavailable", "Lore code browsing is unavailable")
		return
	}
	credential, err := api.credential(request, repository, actor, loreclient.ScopeRead)
	if err != nil {
		api.loreError(writer, request, "get Lore read credential", err)
		return
	}
	result, body, err := api.code.File(
		request.Context(), api.repositoryRef(repository), revision, path, credential, maxFileBytes,
	)
	if err != nil {
		api.loreError(writer, request, "read Lore file", err)
		return
	}
	if result.Truncated {
		api.problem(writer, http.StatusRequestEntityTooLarge, "file_too_large", "The Lore file exceeds the download limit")
		return
	}
	if result.Kind != "file" {
		api.problem(writer, http.StatusConflict, "not_a_file", "The requested Lore path is not a file")
		return
	}
	contentType := "application/octet-stream"
	if !result.Binary {
		contentType = "text/plain; charset=utf-8"
	}
	writer.Header().Set("Content-Type", contentType)
	writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(body)
}

func (api *API) history(writer http.ResponseWriter, request *http.Request) {
	repository, actor, ok := api.visibleRepository(writer, request)
	if !ok {
		return
	}
	revision, ok := api.resolveRevision(writer, request, repository, actor)
	if !ok {
		return
	}
	branch, err := optionalBranch(request.URL.Query().Get("branch"))
	if err != nil {
		api.problem(writer, http.StatusBadRequest, "invalid_branch", "The Lore branch is invalid")
		return
	}
	if branch == "" && request.URL.Query().Get("revision") == "" {
		branch, err = normalizeBranch(repository.DefaultBranch)
		if err != nil {
			api.problem(writer, http.StatusBadRequest, "invalid_branch", "The Lore branch is invalid")
			return
		}
	}
	if api.code == nil {
		api.problem(writer, http.StatusServiceUnavailable, "lore_unavailable", "Lore history is unavailable")
		return
	}
	credential, err := api.credential(request, repository, actor, loreclient.ScopeRead)
	if err != nil {
		api.loreError(writer, request, "get Lore read credential", err)
		return
	}
	entries, err := api.code.RevisionHistory(
		request.Context(), api.repositoryRef(repository), historyRevision(request, revision), branch, credential,
		boundedInt(request.URL.Query().Get("limit"), 50, maxHistoryLimit),
	)
	if err != nil {
		api.loreError(writer, request, "read Lore revision history", err)
		return
	}
	entries, hasMore := historyWindow(entries, boundedInt(request.URL.Query().Get("limit"), 50, maxHistoryLimit))
	api.resolveRevisionAuthors(request.Context(), entries)
	writeJSON(writer, http.StatusOK, map[string]any{"revision": revision, "entries": entries,
		"hasMore": hasMore})
}

func historyRevision(request *http.Request, resolved string) string {
	if request.URL.Query().Get("revision") == "" {
		return ""
	}
	return resolved
}

func historyWindow[T any](entries []T, limit int) ([]T, bool) {
	if limit < 1 {
		limit = 1
	}
	if len(entries) <= limit {
		return entries, false
	}
	return entries[:limit], true
}

func (api *API) revision(writer http.ResponseWriter, request *http.Request) {
	repository, actor, ok := api.visibleRepository(writer, request)
	if !ok {
		return
	}
	revision, err := normalizeRevision(request.PathValue("revision"))
	if err != nil {
		api.problem(writer, http.StatusBadRequest, "invalid_revision", "The Lore revision is invalid")
		return
	}
	if api.code == nil {
		api.problem(writer, http.StatusServiceUnavailable, "lore_unavailable", "Lore revision details are unavailable")
		return
	}
	credential, err := api.credential(request, repository, actor, loreclient.ScopeRead)
	if err != nil {
		api.loreError(writer, request, "get Lore read credential", err)
		return
	}
	result, err := api.code.RevisionInfo(request.Context(), api.repositoryRef(repository), revision, credential)
	if err != nil {
		api.loreError(writer, request, "read Lore revision", err)
		return
	}
	result.Author = api.resolveRevisionAuthor(request.Context(), result.Author, make(map[string]string, 1))
	writeJSON(writer, http.StatusOK, result)
}

func (api *API) resolveRevisionAuthors(ctx context.Context, entries []loreclient.RevisionHistoryEntry) {
	cache := make(map[string]string, len(entries))
	for index := range entries {
		entries[index].Author = api.resolveRevisionAuthor(ctx, entries[index].Author, cache)
	}
}

func (api *API) resolveRevisionAuthor(ctx context.Context, raw string, cache map[string]string) string {
	authorID := strings.TrimSpace(raw)
	if authorID == "" {
		return ""
	}
	if author, ok := cache[authorID]; ok {
		return author
	}
	author := authorID
	if api.users != nil {
		user, err := api.users.ActiveUser(ctx, authorID)
		if err == nil && strings.TrimSpace(user.Username) != "" {
			author = strings.TrimSpace(user.Username)
		}
	}
	cache[authorID] = author
	return author
}

func (api *API) diff(writer http.ResponseWriter, request *http.Request) {
	repository, actor, ok := api.visibleRepository(writer, request)
	if !ok {
		return
	}
	source, err := normalizeRevision(request.URL.Query().Get("source"))
	if err != nil {
		api.problem(writer, http.StatusBadRequest, "invalid_source_revision", "The source Lore revision is invalid")
		return
	}
	target, err := normalizeRevision(request.URL.Query().Get("target"))
	if err != nil {
		api.problem(writer, http.StatusBadRequest, "invalid_target_revision", "The target Lore revision is invalid")
		return
	}
	paths, err := normalizePaths(request.URL.Query()["path"])
	if err != nil {
		api.problem(writer, http.StatusBadRequest, "invalid_path", "A diff path is invalid")
		return
	}
	if api.code == nil {
		api.problem(writer, http.StatusServiceUnavailable, "lore_unavailable", "Lore diffs are unavailable")
		return
	}
	credential, err := api.credential(request, repository, actor, loreclient.ScopeRead)
	if err != nil {
		api.loreError(writer, request, "get Lore read credential", err)
		return
	}
	result, err := api.code.RevisionDiff(
		request.Context(), api.repositoryRef(repository), source, target, paths, credential,
		maxDiffFiles, maxDiffPatchBytes,
	)
	if err != nil {
		api.loreError(writer, request, "read Lore revision diff", err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (api *API) resolveRevision(
	writer http.ResponseWriter,
	request *http.Request,
	repository collab.Repository,
	actor *platform.User,
) (string, bool) {
	if revision := request.URL.Query().Get("revision"); revision != "" {
		value, err := normalizeRevision(revision)
		if err != nil {
			api.problem(writer, http.StatusBadRequest, "invalid_revision", "The Lore revision is invalid")
			return "", false
		}
		return value, true
	}
	branch := request.URL.Query().Get("branch")
	if branch == "" {
		branch = repository.DefaultBranch
	}
	branch, err := normalizeBranch(branch)
	if err != nil || api.lore == nil {
		api.problem(writer, http.StatusBadRequest, "invalid_branch", "The Lore branch is invalid")
		return "", false
	}
	credential, err := api.credential(request, repository, actor, loreclient.ScopeRead)
	if err != nil {
		api.loreError(writer, request, "get Lore read credential", err)
		return "", false
	}
	branches, err := api.lore.Branches(request.Context(), api.repositoryRef(repository), credential)
	if err != nil {
		api.loreError(writer, request, "list Lore branches", err)
		return "", false
	}
	for _, item := range branches {
		if item.Name == branch && !item.Archived && item.LatestRevision != "" {
			value, err := normalizeRevision(item.LatestRevision)
			if err == nil {
				return value, true
			}
		}
	}
	api.problem(writer, http.StatusNotFound, "branch_not_found", "The Lore branch was not found")
	return "", false
}

func optionalBranch(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	return normalizeBranch(value)
}

func normalizePaths(values []string) ([]string, error) {
	if len(values) > maxDiffPathCount {
		return nil, errInvalidPath
	}
	paths := make([]string, 0, len(values))
	for _, value := range values {
		path, err := normalizePath(value)
		if err != nil || path == "" {
			return nil, errInvalidPath
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func (api *API) repositoryRef(repository collab.Repository) loreclient.RepositoryRef {
	return loreclient.RepositoryRef{
		CacheKey:         repository.ID,
		URL:              repository.LoreURL,
		LoreRepositoryID: repository.LoreRepositoryID,
		DefaultBranch:    repository.DefaultBranch,
	}
}

func (api *API) credential(
	request *http.Request,
	repository collab.Repository,
	actor *platform.User,
	scope loreclient.Scope,
) (loreclient.Credential, error) {
	if api.credentials == nil {
		return loreclient.Credential{}, loreclient.ErrCredentialUnavailable
	}
	principal := loreclient.ServicePrincipal(loreclient.ServicePurposePublicReader, api.publicReaderSubject)
	if actor != nil {
		principal = loreclient.UserPrincipal(actor.ID)
	}
	ref := api.repositoryRef(repository)
	return api.credentials.ForRepository(request.Context(), loreclient.CredentialRequest{
		Principal:  principal,
		Repository: ref,
		Partition:  ref.CanonicalPartition(),
		Scope:      scope,
	})
}

func (api *API) storeError(writer http.ResponseWriter, request *http.Request, operation string, err error) {
	switch {
	case errors.Is(err, platform.ErrNotFound):
		api.problem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
	case errors.Is(err, platform.ErrForbidden):
		api.problem(writer, http.StatusForbidden, "forbidden", "This operation is not permitted")
	default:
		api.logger.Error(operation, "error", err, "method", request.Method, "path", request.URL.Path)
		api.problem(writer, http.StatusInternalServerError, "internal_error", "The request could not be completed")
	}
}

func (api *API) loreError(writer http.ResponseWriter, request *http.Request, operation string, err error) {
	if errors.Is(err, errInvalidPath) || errors.Is(err, errInvalidRevision) {
		api.problem(writer, http.StatusBadRequest, "invalid_input", "The Lore request is invalid")
		return
	}
	if errors.Is(err, loreclient.ErrNotFound) {
		api.problem(writer, http.StatusNotFound, "not_found", "The requested Lore path was not found")
		return
	}
	if errors.Is(err, request.Context().Err()) {
		return
	}
	api.logger.Error(operation, "error", err, "method", request.Method, "path", request.URL.Path)
	api.problem(writer, http.StatusBadGateway, "lore_unavailable", "Lore did not complete the request")
}

func (api *API) problem(writer http.ResponseWriter, status int, code string, detail string) {
	writer.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"error": map[string]string{"code": code, "detail": detail},
	})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
