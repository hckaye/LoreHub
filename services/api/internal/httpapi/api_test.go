package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/auth"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type fakeStore struct {
	repositories []platform.Repository
	user         platform.User
}

func (store fakeStore) EnsureUser(context.Context, auth.Principal) (platform.User, error) {
	return store.user, nil
}

func (store fakeStore) CreateOrganization(
	context.Context,
	platform.User,
	platform.CreateOrganizationInput,
) (platform.Organization, error) {
	return platform.Organization{}, nil
}

func (store fakeStore) RegisterRepository(
	context.Context,
	platform.User,
	string,
	platform.RegisterRepositoryInput,
) (platform.Repository, error) {
	return platform.Repository{}, nil
}

func (store fakeStore) ExploreRepositories(context.Context, int) ([]platform.Repository, error) {
	return store.repositories, nil
}

func (store fakeStore) PublicRepository(context.Context, string, string) (platform.Repository, error) {
	return platform.Repository{}, platform.ErrNotFound
}

func (store fakeStore) RepositoryForWrite(
	context.Context,
	platform.User,
	string,
	string,
) (platform.Repository, error) {
	return platform.Repository{}, platform.ErrForbidden
}

func (store fakeStore) ListPublicIssues(context.Context, string, string, string) ([]platform.Issue, error) {
	return []platform.Issue{}, nil
}

func (store fakeStore) CreateIssue(
	context.Context,
	platform.User,
	string,
	string,
	platform.CreateIssueInput,
) (platform.Issue, error) {
	return platform.Issue{}, nil
}

func (store fakeStore) ListPublicMergeRequests(
	context.Context,
	string,
	string,
	string,
) ([]platform.MergeRequest, error) {
	return []platform.MergeRequest{}, nil
}

func (store fakeStore) CreateMergeRequest(
	context.Context,
	platform.User,
	string,
	string,
	platform.CreateMergeRequestInput,
) (platform.MergeRequest, error) {
	return platform.MergeRequest{}, nil
}

func (store fakeStore) ListPublicCIRuns(context.Context, string, string) ([]platform.CIRun, error) {
	return []platform.CIRun{}, nil
}

type fakeLore struct{}

func (fakeLore) RepositoryInfo(context.Context, string, string) (loreclient.Repository, error) {
	return loreclient.Repository{}, nil
}

func (fakeLore) Branches(context.Context, loreclient.RepositoryRef, string) ([]loreclient.Branch, error) {
	return []loreclient.Branch{}, nil
}

type healthy struct{}

func (healthy) Ping(context.Context) error { return nil }

func TestExploreRepositories(t *testing.T) {
	t.Parallel()
	handler := New(
		fakeStore{repositories: []platform.Repository{{ID: "repository-1", Slug: "lore"}}},
		fakeLore{},
		auth.DisabledAuthenticator{},
		healthy{},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/explore/repositories", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	if response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("security headers were not applied")
	}
}
