package branches

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/collab"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

const testRevision = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

type fakeStore struct {
	access           collab.Access
	rules            []collab.BranchRule
	ruleErr          error
	observedCreates  *int
	observedArchives *int
}

func (store fakeStore) LookupRepository(
	context.Context,
	*platform.User,
	string,
	string,
) (collab.Repository, error) {
	return collab.Repository{
		ID: "repository-id", Owner: "lore-demo", Slug: "sample", DefaultBranch: "main",
		LoreRepositoryID: "0123456789abcdef0123456789abcdef",
		LoreURL:          "lore://localhost/0123456789abcdef0123456789abcdef",
	}, nil
}

func (store fakeStore) RepositoryPermission(
	context.Context,
	platform.User,
	collab.Repository,
) (collab.Access, error) {
	return store.access, nil
}

func (store fakeStore) ListBranchRules(context.Context, string) ([]collab.BranchRule, error) {
	return store.rules, store.ruleErr
}

func (store fakeStore) RecordLoreBranchCreation(context.Context, string, string, string, string, string) error {
	if store.observedCreates != nil {
		*store.observedCreates++
	}
	return nil
}

func (store fakeStore) RecordLoreBranchDeletion(context.Context, string, string, string, string, string) error {
	if store.observedArchives != nil {
		*store.observedArchives++
	}
	return nil
}

type fakeActors struct{}

func (fakeActors) ResolveActor(http.ResponseWriter, *http.Request) (platform.User, bool) {
	return platform.User{ID: "user-id", Username: "demo"}, true
}

type fakeCredentials struct{}

func (fakeCredentials) ForRepository(
	_ context.Context,
	request loreclient.CredentialRequest,
) (loreclient.Credential, error) {
	return loreclient.Credential{
		Partition: request.Partition, Scope: request.Scope, Identity: request.Principal.UserID,
		Principal: request.Principal, InsecureDevelopment: true,
	}, nil
}

type fakeLore struct {
	branches []loreclient.Branch
	created  loreclient.Branch
	archived loreclient.Branch
}

func (lore *fakeLore) RepositoryInfo(
	context.Context,
	string,
	loreclient.Credential,
) (loreclient.Repository, error) {
	return loreclient.Repository{}, nil
}

func (lore *fakeLore) Branches(
	context.Context,
	loreclient.RepositoryRef,
	loreclient.Credential,
) ([]loreclient.Branch, error) {
	return append([]loreclient.Branch(nil), lore.branches...), nil
}

func (lore *fakeLore) CreateBranch(
	_ context.Context,
	_ loreclient.RepositoryRef,
	name string,
	category string,
	revision string,
	_ loreclient.Credential,
) (loreclient.Branch, error) {
	lore.created = loreclient.Branch{
		ID: "feature-id", Name: name, Category: category, LatestRevision: revision,
	}
	return lore.created, nil
}

func (lore *fakeLore) ArchiveBranch(
	_ context.Context,
	_ loreclient.RepositoryRef,
	branch loreclient.Branch,
	_ loreclient.Credential,
) error {
	lore.archived = branch
	return nil
}

func TestCreateBranchUsesExactLoreSourceRevision(t *testing.T) {
	lore := &fakeLore{branches: []loreclient.Branch{{ID: "main-id", Name: "main", LatestRevision: testRevision}}}
	observed := 0
	handler := testHandler(fakeStore{
		access: collab.Access{Permission: collab.PermWrite}, observedCreates: &observed,
	}, lore)
	body := `{"name":"feature/branches","category":"feature","sourceBranch":"main","sourceRevision":"` +
		testRevision + `"}`
	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/repositories/lore-demo/sample/branches", strings.NewReader(body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if lore.created.Name != "feature/branches" || lore.created.LatestRevision != testRevision {
		t.Fatalf("created branch = %+v", lore.created)
	}
	if observed != 1 {
		t.Fatalf("creation observations = %d", observed)
	}
}

func TestCreateBranchRejectsStaleSourceAndProtectedName(t *testing.T) {
	lore := &fakeLore{branches: []loreclient.Branch{{ID: "main-id", Name: "main", LatestRevision: testRevision}}}
	for _, test := range []struct {
		name  string
		store fakeStore
		body  string
	}{
		{
			name:  "stale source",
			store: fakeStore{access: collab.Access{Permission: collab.PermWrite}},
			body: `{"name":"feature/a","sourceBranch":"main","sourceRevision":"` +
				strings.Repeat("a", 64) + `"}`,
		},
		{
			name: "protected branch",
			store: fakeStore{
				access: collab.Access{Permission: collab.PermWrite},
				rules:  []collab.BranchRule{{Pattern: "release/*", BlockDirectPush: true}},
			},
			body: `{"name":"release/1","sourceBranch":"main","sourceRevision":"` + testRevision + `"}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost,
				"/api/v1/repositories/lore-demo/sample/branches", strings.NewReader(test.body))
			response := httptest.NewRecorder()
			testHandler(test.store, lore).ServeHTTP(response, request)
			if response.Code != http.StatusConflict {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestArchiveBranchRejectsDefaultAndArchivesFeature(t *testing.T) {
	lore := &fakeLore{branches: []loreclient.Branch{
		{ID: "main-id", Name: "main", LatestRevision: testRevision},
		{ID: "feature-id", Name: "feature/a", LatestRevision: testRevision},
	}}
	observed := 0
	handler := testHandler(fakeStore{
		access: collab.Access{Permission: collab.PermWrite}, observedArchives: &observed,
	}, lore)
	defaultRequest := httptest.NewRequest(http.MethodDelete,
		"/api/v1/repositories/lore-demo/sample/branches/main", nil)
	defaultResponse := httptest.NewRecorder()
	handler.ServeHTTP(defaultResponse, defaultRequest)
	if defaultResponse.Code != http.StatusConflict {
		t.Fatalf("default status = %d", defaultResponse.Code)
	}
	request := httptest.NewRequest(http.MethodDelete,
		"/api/v1/repositories/lore-demo/sample/branches/feature/a", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if lore.archived.ID != "feature-id" {
		t.Fatalf("archived branch = %+v", lore.archived)
	}
	if observed != 1 {
		t.Fatalf("archive observations = %d", observed)
	}
}

func TestBranchMutationRequiresWritePermission(t *testing.T) {
	lore := &fakeLore{branches: []loreclient.Branch{{ID: "main-id", Name: "main", LatestRevision: testRevision}}}
	handler := testHandler(fakeStore{access: collab.Access{Permission: collab.PermRead}}, lore)
	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/repositories/lore-demo/sample/branches", strings.NewReader(`{}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestBranchMutationFailsClosedWhenRulesCannotBeRead(t *testing.T) {
	lore := &fakeLore{branches: []loreclient.Branch{{ID: "main-id", Name: "main", LatestRevision: testRevision}}}
	handler := testHandler(fakeStore{
		access: collab.Access{Permission: collab.PermWrite}, ruleErr: errors.New("database unavailable"),
	}, lore)
	body := `{"name":"feature/a","sourceBranch":"main","sourceRevision":"` + testRevision + `"}`
	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/repositories/lore-demo/sample/branches", strings.NewReader(body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func testHandler(store fakeStore, lore *fakeLore) http.Handler {
	mux := http.NewServeMux()
	Register(
		mux, store, store, fakeActors{}, lore, fakeCredentials{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	return mux
}
