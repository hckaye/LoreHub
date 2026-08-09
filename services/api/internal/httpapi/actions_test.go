package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/auth"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
	"github.com/lorehub/lorehub/services/api/internal/runner"
)

type fakeActionsStore struct {
	access    runner.RepositoryAccess
	workflows []runner.WorkflowRecord
	runs      []runner.RunRecord
	dispatch  runner.RunRecord
	cancel    runner.RunRecord
	rerun     runner.RunRecord
}

func (store *fakeActionsStore) RepositoryForActions(
	context.Context, string, string, string,
) (runner.RepositoryAccess, error) {
	return store.access, nil
}

func (store *fakeActionsStore) ListWorkflows(context.Context, string, string, string) ([]runner.WorkflowRecord, error) {
	return store.workflows, nil
}

func (store *fakeActionsStore) ListActionRuns(
	context.Context, string, string, string, runner.RunFilter,
) ([]runner.RunRecord, int, error) {
	return store.runs, len(store.runs), nil
}

func (store *fakeActionsStore) ActionRunDetail(
	context.Context, string, string, int64, string,
) (runner.RunDetail, error) {
	return runner.RunDetail{}, nil
}

func (store *fakeActionsStore) DispatchWorkflow(
	context.Context, runner.RepositoryAccess, string, string, string, []byte, string,
) (runner.RunRecord, error) {
	return store.dispatch, nil
}

func (store *fakeActionsStore) CancelActionRun(
	context.Context, runner.RepositoryAccess, int64, string,
) (runner.RunRecord, error) {
	return store.cancel, nil
}

func (store *fakeActionsStore) RerunActionRun(
	context.Context, runner.RepositoryAccess, int64, string,
) (runner.RunRecord, error) {
	return store.rerun, nil
}

func (store *fakeActionsStore) OpenActionJobLog(
	context.Context, string, string, string, string,
) (runner.FileDownload, error) {
	return runner.FileDownload{}, errors.New("not used")
}

func (store *fakeActionsStore) OpenActionArtifact(
	context.Context, string, string, string, string,
) (runner.FileDownload, error) {
	return runner.FileDownload{}, errors.New("not used")
}

func TestActionsRoutesExposeMetadataAndDispatchLocation(t *testing.T) {
	store := &fakeActionsStore{
		access:    runner.RepositoryAccess{CanRead: true, CanWrite: true, ID: "repo-1"},
		workflows: []runner.WorkflowRecord{{ID: "workflow-1", Path: ".github/workflows/ci.yml", State: "active"}},
		dispatch:  runner.RunRecord{ID: "run-1", RunNumber: 7, EventName: "workflow_dispatch"},
	}
	handler := New(
		fakeStore{user: platformUserForActionsTest()},
		actionsLore{branches: []loreclient.Branch{{Name: "main", LatestRevision: "revision-7"}}},
		staticAuthenticator{principal: auth.Principal{Subject: "subject"}},
		healthy{},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithActions(store),
	)

	metadataRequest := httptest.NewRequest(http.MethodGet, "/api/v1/repositories/acme/demo/actions/workflows", nil)
	metadataResponse := httptest.NewRecorder()
	handler.ServeHTTP(metadataResponse, metadataRequest)
	if metadataResponse.Code != http.StatusOK || !strings.Contains(metadataResponse.Body.String(), "workflows") {
		t.Fatalf("unexpected workflow response: %d %s", metadataResponse.Code, metadataResponse.Body.String())
	}

	dispatchRequest := httptest.NewRequest(http.MethodPost,
		"/api/v1/repositories/acme/demo/actions/workflows/workflow-1/dispatches",
		strings.NewReader(`{"branch":"main"}`))
	dispatchRequest.Header.Set("Authorization", "Bearer test-token")
	dispatchResponse := httptest.NewRecorder()
	handler.ServeHTTP(dispatchResponse, dispatchRequest)
	if dispatchResponse.Code != http.StatusCreated {
		t.Fatalf("unexpected dispatch response: %d %s", dispatchResponse.Code, dispatchResponse.Body.String())
	}
	if got := dispatchResponse.Header().Get("Location"); got != "/api/v1/repositories/acme/demo/actions/runs/7" {
		t.Fatalf("unexpected dispatch Location: %q", got)
	}
}

func platformUserForActionsTest() platform.User {
	return platform.User{ID: "user-1", Username: "actions", DisplayName: "Actions"}
}

type actionsLore struct {
	branches []loreclient.Branch
}

func (actionsLore) RepositoryInfo(context.Context, string, string) (loreclient.Repository, error) {
	return loreclient.Repository{}, nil
}

func (lore actionsLore) Branches(context.Context, loreclient.RepositoryRef, string) ([]loreclient.Branch, error) {
	return lore.branches, nil
}
