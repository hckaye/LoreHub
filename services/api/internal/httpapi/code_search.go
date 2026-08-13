package httpapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

const (
	codeSearchMaxFiles      = 2_000
	codeSearchMaxFileBytes  = int64(512 << 10)
	codeSearchMaxTotalBytes = int64(32 << 20)
	codeSearchTreeLimit     = 2_000
)

var codeSearchRepositoryPart = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*$`)

type codeSearchQuery struct {
	Owner      string
	Repository string
	Terms      []string
}

var (
	errCodeSearchRepositoryRequired = errors.New(
		"code search requires a repo:OWNER/SLUG qualifier, for example repo:acme/lore needle",
	)
	errCodeSearchRepositoryInvalid = errors.New("the code search repo qualifier must use repo:OWNER/SLUG")
	errCodeSearchTermRequired      = errors.New("code search requires a term after repo:OWNER/SLUG")
)

func parseCodeSearchQuery(value string) (codeSearchQuery, error) {
	fields := strings.Fields(value)
	var result codeSearchQuery
	var foundRepository bool
	terms := make([]string, 0, len(fields))
	for _, field := range fields {
		if !strings.HasPrefix(strings.ToLower(field), "repo:") {
			terms = append(terms, field)
			continue
		}
		if foundRepository {
			return codeSearchQuery{}, errCodeSearchRepositoryInvalid
		}
		owner, repository, ok := strings.Cut(field[5:], "/")
		if !ok || owner == "" || repository == "" || strings.Contains(repository, "/") ||
			!codeSearchRepositoryPart.MatchString(owner) || !codeSearchRepositoryPart.MatchString(repository) {
			return codeSearchQuery{}, errCodeSearchRepositoryInvalid
		}
		result.Owner = owner
		result.Repository = repository
		foundRepository = true
	}
	if !foundRepository {
		return codeSearchQuery{}, errCodeSearchRepositoryRequired
	}
	result.Terms = normalizedCodeSearchTerms(terms)
	if len(result.Terms) == 0 {
		return codeSearchQuery{}, errCodeSearchTermRequired
	}
	return result, nil
}

func normalizedCodeSearchTerms(values []string) []string {
	terms := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		term := strings.ToLower(strings.TrimSpace(value))
		if term == "" {
			continue
		}
		if _, ok := seen[term]; ok {
			continue
		}
		seen[term] = struct{}{}
		terms = append(terms, term)
	}
	return terms
}

func (api *API) codeSearch(
	writer http.ResponseWriter,
	request *http.Request,
	viewer *platform.User,
	query codeSearchQuery,
) {
	repository, err := api.repositoryForCodeSearch(request.Context(), viewer, query.Owner, query.Repository)
	if err != nil {
		api.platformError(writer, request, "get code search repository", err)
		return
	}
	if repository.LoreURL == "" || repository.LoreRepositoryID == "" {
		writeProblem(writer, http.StatusBadGateway, "lore_unavailable", "The Lore repository is not configured")
		return
	}
	client, ok := api.lore.(loreclient.CodeClient)
	if !ok {
		writeProblem(writer, http.StatusServiceUnavailable, "lore_unavailable", "Lore code search is unavailable")
		return
	}
	ref := loreclient.RepositoryRef{
		CacheKey: repository.ID, URL: repository.LoreURL,
		LoreRepositoryID: repository.LoreRepositoryID, DefaultBranch: repository.DefaultBranch,
	}
	principal := loreclient.ServicePrincipal(loreclient.ServicePurposePublicReader, api.serviceSubjects.PublicReader)
	if viewer != nil {
		principal = loreclient.UserPrincipal(viewer.ID)
	}
	credential, err := api.loreCredential(request.Context(), ref, principal, loreclient.ScopeRead)
	if err != nil {
		api.codeSearchLoreError(writer, request, "get code search Lore credential", err)
		return
	}
	branches, err := api.lore.Branches(request.Context(), ref, credential)
	if err != nil {
		api.codeSearchLoreError(writer, request, "list code search Lore branches", err)
		return
	}
	revision, ok := latestRevision(branches, repository.DefaultBranch)
	if !ok {
		writeProblem(writer, http.StatusNotFound, "branch_not_found", "The repository default branch was not found")
		return
	}
	codeResults, err := scanCodeSearch(request.Context(), client, ref, revision, credential, query.Terms)
	if err != nil {
		if errors.Is(err, request.Context().Err()) {
			return
		}
		api.codeSearchLoreError(writer, request, "scan Lore repository for code search", err)
		return
	}
	codeResults.Revision = revision
	writeJSON(writer, http.StatusOK, platform.SearchResults{
		Repositories:  []platform.Repository{},
		Organizations: []platform.OrganizationView{},
		Users:         []platform.UserSearchResult{},
		Issues:        []platform.GlobalWorkItem{},
		PullRequests:  []platform.GlobalWorkItem{},
		Code:          &codeResults,
		Counts:        platform.SearchCounts{Code: int64(len(codeResults.Files))},
		Page:          1,
		PerPage:       20,
	})
}

func (api *API) codeSearchLoreError(
	writer http.ResponseWriter,
	request *http.Request,
	operation string,
	err error,
) {
	if errors.Is(err, loreclient.ErrNotFound) {
		writeProblem(writer, http.StatusNotFound, "not_found", "The requested Lore path was not found")
		return
	}
	if errors.Is(err, request.Context().Err()) {
		return
	}
	api.logger.Error(operation, "error", err, "method", request.Method, "path", request.URL.Path)
	writeProblem(writer, http.StatusBadGateway, "lore_unavailable", "Lore did not complete the request")
}

func (api *API) repositoryForCodeSearch(
	ctx context.Context,
	viewer *platform.User,
	owner string,
	repository string,
) (platform.Repository, error) {
	if reader, ok := api.store.(RepositoryReader); ok {
		return reader.RepositoryForRead(ctx, viewer, owner, repository)
	}
	if viewer == nil {
		return api.store.PublicRepository(ctx, owner, repository)
	}
	return platform.Repository{}, errors.New("authenticated repository visibility is unavailable")
}

func scanCodeSearch(
	ctx context.Context,
	client loreclient.CodeClient,
	repository loreclient.RepositoryRef,
	revision string,
	credential loreclient.Credential,
	terms []string,
) (platform.CodeSearchResults, error) {
	result := platform.CodeSearchResults{Files: []platform.CodeSearchFile{}}
	terms = normalizedCodeSearchTerms(terms)
	if len(terms) == 0 {
		return result, nil
	}
	directories := []string{""}
	seenDirectories := map[string]struct{}{"": {}}
	var scannedFiles int
	var scannedBytes int64
	for directoryIndex := 0; directoryIndex < len(directories); directoryIndex++ {
		if err := ctx.Err(); err != nil {
			return platform.CodeSearchResults{}, err
		}
		tree, err := client.Tree(ctx, repository, revision, directories[directoryIndex], credential, codeSearchTreeLimit)
		if err != nil {
			return platform.CodeSearchResults{}, err
		}
		if tree.HasMore {
			result.Truncated = true
		}
		entries := append([]loreclient.TreeEntry(nil), tree.Entries...)
		sort.Slice(entries, func(left, right int) bool { return entries[left].Path < entries[right].Path })
		for _, entry := range entries {
			switch entry.Kind {
			case "directory":
				if entry.Path != "" {
					if _, seen := seenDirectories[entry.Path]; !seen {
						seenDirectories[entry.Path] = struct{}{}
						directories = append(directories, entry.Path)
					}
				}
			case "file":
				if scannedFiles >= codeSearchMaxFiles {
					result.Truncated = true
					return result, nil
				}
				scannedFiles++
				if entry.Size > uint64(codeSearchMaxFileBytes) {
					result.Truncated = true
					continue
				}
				if scannedBytes+int64(entry.Size) > codeSearchMaxTotalBytes {
					result.Truncated = true
					return result, nil
				}
				file, body, err := client.File(ctx, repository, revision, entry.Path, credential, codeSearchMaxFileBytes)
				if err != nil {
					return platform.CodeSearchResults{}, err
				}
				if file.Truncated {
					result.Truncated = true
					continue
				}
				if int64(len(body)) > codeSearchMaxFileBytes {
					result.Truncated = true
					continue
				}
				if scannedBytes+int64(len(body)) > codeSearchMaxTotalBytes {
					result.Truncated = true
					return result, nil
				}
				scannedBytes += int64(len(body))
				if isCodeSearchBinary(file, body) {
					continue
				}
				matches, matchCount := codeSearchMatches(string(body), terms)
				if matchCount > 0 {
					result.Files = append(result.Files, platform.CodeSearchFile{
						Path: entry.Path, MatchCount: matchCount, Matches: matches,
					})
				}
			}
		}
	}
	return result, nil
}

func isCodeSearchBinary(file loreclient.File, body []byte) bool {
	if file.BinaryKnown {
		return file.Binary
	}
	return bytes.IndexByte(body, 0) >= 0 || !utf8.Valid(body)
}

func codeSearchMatches(content string, terms []string) ([]platform.CodeSearchMatch, int) {
	lines := strings.Split(content, "\n")
	matches := make([]platform.CodeSearchMatch, 0)
	matchCount := 0
	for index, line := range lines {
		lowerLine := strings.ToLower(line)
		lineMatches := true
		lineMatchCount := 0
		for _, term := range terms {
			occurrences := strings.Count(lowerLine, term)
			if occurrences == 0 {
				lineMatches = false
				break
			}
			lineMatchCount += occurrences
		}
		if !lineMatches {
			continue
		}
		matches = append(matches, platform.CodeSearchMatch{
			LineNumber: index + 1,
			Snippet:    strings.TrimSpace(line),
		})
		matchCount += lineMatchCount
	}
	return matches, matchCount
}
