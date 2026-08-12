package platform

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"
)

const (
	defaultSearchPerPage = 20
	maxSearchPerPage     = 50
	maxSearchQueryRunes  = 160
	maxSearchPage        = 100_000
)

func (store *Store) Search(
	ctx context.Context,
	viewer *User,
	query string,
	kind string,
	page int,
	perPage int,
) (SearchResults, error) {
	query = strings.TrimSpace(query)
	offset, err := validateSearchInput(query, kind, page, perPage)
	if err != nil {
		return SearchResults{}, err
	}
	viewerID := ""
	if viewer != nil {
		viewerID = viewer.ID
	}
	result := emptySearchResults(page, perPage)
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return SearchResults{}, fmt.Errorf("begin search: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	pattern := globalWorkItemPattern(query)
	result.Counts, err = searchCounts(ctx, tx, query, viewerID, pattern)
	if err != nil {
		return SearchResults{}, err
	}
	if kind == "all" || kind == "repositories" {
		result.Repositories, err = searchRepositories(ctx, tx, query, viewerID, pattern, perPage, offset)
		if err != nil {
			return SearchResults{}, err
		}
	}
	if kind == "all" || kind == "organizations" {
		result.Organizations, err = searchOrganizations(ctx, tx, query, viewerID, pattern, perPage, offset)
		if err != nil {
			return SearchResults{}, err
		}
	}
	if kind == "all" || kind == "users" {
		result.Users, err = searchUsers(ctx, tx, query, pattern, perPage, offset)
		if err != nil {
			return SearchResults{}, err
		}
	}
	if kind == "all" || kind == "issues" {
		result.Issues, err = searchIssueItems(ctx, tx, query, viewerID, pattern, perPage, offset)
		if err != nil {
			return SearchResults{}, err
		}
	}
	if kind == "all" || kind == "pulls" {
		result.PullRequests, err = searchPullRequestItems(
			ctx, tx, query, viewerID, pattern, perPage, offset,
		)
		if err != nil {
			return SearchResults{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return SearchResults{}, fmt.Errorf("commit search snapshot: %w", err)
	}
	return result, nil
}

func emptySearchResults(page int, perPage int) SearchResults {
	return SearchResults{
		Repositories: []Repository{}, Organizations: []OrganizationView{},
		Users: []UserSearchResult{}, Issues: []GlobalWorkItem{},
		PullRequests: []GlobalWorkItem{}, Page: page, PerPage: perPage,
	}
}

func validateSearchInput(query string, kind string, page int, perPage int) (int, error) {
	if query == "" || len([]rune(query)) > maxSearchQueryRunes || searchQueryHasControl(query) ||
		!validSearchKind(kind) ||
		page < 1 || page > maxSearchPage || perPage < 1 || perPage > maxSearchPerPage {
		return 0, ErrInvalidInput
	}
	maximum := int(^uint(0) >> 1)
	if page-1 > maximum/perPage {
		return 0, ErrInvalidInput
	}
	return (page - 1) * perPage, nil
}

func searchQueryHasControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}

func validSearchKind(kind string) bool {
	switch kind {
	case "all", "repositories", "organizations", "users", "issues", "pulls":
		return true
	default:
		return false
	}
}
