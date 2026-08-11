package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/authz"
)

type policyCaptureStore struct {
	AuthorizationStore
	check       authz.PolicyCheck
	observation loreObservationRequest
	prepared    loreObservationRequest
}

func (store *policyCaptureStore) CheckPolicy(
	_ context.Context,
	check authz.PolicyCheck,
) (authz.PolicyDecision, error) {
	store.check = check
	return authz.PolicyDecision{Allowed: true}, nil
}

func (store *policyCaptureStore) PrepareLoreBranchCreation(
	_ context.Context,
	actorID string,
	loreRepositoryID string,
	branchID string,
	branchName string,
) error {
	store.prepared = loreObservationRequest{
		UserID: actorID, ResourceID: "urc-" + loreRepositoryID, Operation: authz.OperationBranchCreate,
		BranchID: branchID, BranchName: branchName,
	}
	return nil
}

func (store *policyCaptureStore) RecordLoreBranchCreation(
	_ context.Context,
	actorID string,
	loreRepositoryID string,
	branchID string,
	branchName string,
	revision string,
) error {
	store.observation = loreObservationRequest{
		UserID: actorID, ResourceID: "urc-" + loreRepositoryID, Operation: authz.OperationBranchCreate,
		BranchID: branchID, BranchName: branchName, Revision: &revision,
	}
	return nil
}

func (store *policyCaptureStore) RecordLoreBranchPush(
	context.Context, string, string, string, string,
) error {
	return nil
}

func (store *policyCaptureStore) RecordLoreBranchDeletion(
	_ context.Context,
	actorID string,
	loreRepositoryID string,
	branchID string,
	branchName string,
	revision string,
) error {
	store.observation = loreObservationRequest{
		UserID: actorID, ResourceID: "urc-" + loreRepositoryID, Operation: authz.OperationBranchDelete,
		BranchID: branchID, BranchName: branchName, Revision: &revision,
	}
	return nil
}

func TestInternalPolicyHandlerPreservesBranchName(t *testing.T) {
	store := &policyCaptureStore{}
	handler := NewInternalPolicyHandler(store)
	request := httptest.NewRequest(http.MethodPost, "/internal/lore/policy", strings.NewReader(`{
		"userId":"user-a",
		"resourceId":"urc-0123456789abcdef0123456789abcdef",
		"operation":"branch_create",
		"branchId":"branch-id",
		"branchName":"release/2026.08"
	}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("policy response status = %d, body = %s", response.Code, response.Body.String())
	}
	if store.check.BranchName != "release/2026.08" {
		t.Fatalf("branch name = %q, want exact policy value", store.check.BranchName)
	}
	if store.prepared.BranchID != "branch-id" || store.prepared.BranchName != "release/2026.08" {
		t.Fatalf("prepared branch creation = %+v", store.prepared)
	}
}

func TestInternalObservationRecordsBranchCreation(t *testing.T) {
	store := &policyCaptureStore{}
	handler := NewInternalPolicyHandler(store)
	revision := strings.Repeat("a", 64)
	request := httptest.NewRequest(http.MethodPost, "/internal/lore/observation", strings.NewReader(`{
		"userId":"user-a",
		"resourceId":"urc-0123456789abcdef0123456789abcdef",
		"operation":"branch_create",
		"branchId":"branch-id",
		"branchName":"feature/branch-management",
		"revision":"`+revision+`"
	}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("observation response status = %d, body = %s", response.Code, response.Body.String())
	}
	if store.observation.BranchName != "feature/branch-management" ||
		store.observation.Revision == nil || *store.observation.Revision != revision {
		t.Fatalf("branch creation observation = %+v", store.observation)
	}
}

func TestInternalObservationRejectsBranchCreationWithoutRevision(t *testing.T) {
	store := &policyCaptureStore{}
	handler := NewInternalPolicyHandler(store)
	request := httptest.NewRequest(http.MethodPost, "/internal/lore/observation", strings.NewReader(`{
		"userId":"user-a",
		"resourceId":"urc-0123456789abcdef0123456789abcdef",
		"operation":"branch_create",
		"branchId":"branch-id",
		"branchName":"feature/branch-management"
	}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("observation response status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestInternalObservationRecordsBranchDeletionSnapshot(t *testing.T) {
	store := &policyCaptureStore{}
	handler := NewInternalPolicyHandler(store)
	revision := strings.Repeat("b", 64)
	request := httptest.NewRequest(http.MethodPost, "/internal/lore/observation", strings.NewReader(`{
		"userId":"user-a",
		"resourceId":"urc-0123456789abcdef0123456789abcdef",
		"operation":"branch_delete",
		"branchId":"branch-id",
		"branchName":"feature/archived",
		"revision":"`+revision+`"
	}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("observation response status = %d, body = %s", response.Code, response.Body.String())
	}
	if store.observation.BranchName != "feature/archived" ||
		store.observation.Revision == nil || *store.observation.Revision != revision {
		t.Fatalf("branch deletion observation = %+v", store.observation)
	}
}

func TestInternalObservationRejectsBranchDeletionWithoutSnapshot(t *testing.T) {
	store := &policyCaptureStore{}
	handler := NewInternalPolicyHandler(store)
	request := httptest.NewRequest(http.MethodPost, "/internal/lore/observation", strings.NewReader(`{
		"userId":"user-a",
		"resourceId":"urc-0123456789abcdef0123456789abcdef",
		"operation":"branch_delete",
		"branchId":"branch-id"
	}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("observation response status = %d, body = %s", response.Code, response.Body.String())
	}
}
