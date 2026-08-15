package httpapi

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/authz"
	"github.com/lorehub/lorehub/services/api/internal/platform"
	"github.com/lorehub/lorehub/services/api/internal/servercert"
)

type policyCaptureStore struct {
	AuthorizationStore
	check       authz.PolicyCheck
	observation loreObservationRequest
	prepared    loreObservationRequest
}

type policyHookCaptureStore struct {
	*policyCaptureStore
	serverID        string
	ownedRepository string
	active          bool
	contextServerID string
	activeLookups   int
}

func (store *policyHookCaptureStore) CheckPolicy(
	ctx context.Context,
	check authz.PolicyCheck,
) (authz.PolicyDecision, error) {
	store.contextServerID, _ = ctx.Value(loreHookServerContextKey{}).(string)
	return store.policyCaptureStore.CheckPolicy(ctx, check)
}

func (store *policyHookCaptureStore) ActiveLoreServerForHook(
	_ context.Context,
	serverID string,
) (platform.LoreServer, error) {
	store.activeLookups++
	if !store.active || serverID != store.serverID {
		return platform.LoreServer{}, platform.ErrNotFound
	}
	return platform.LoreServer{ID: serverID, Status: platform.LoreServerStatusActive}, nil
}

func (store *policyHookCaptureStore) LoreServerOwnsRepository(
	_ context.Context,
	serverID string,
	loreRepositoryID string,
) (bool, error) {
	return store.active && serverID == store.serverID && loreRepositoryID == store.ownedRepository, nil
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

func TestInternalPolicyHandlerDeniesOversizedBranchPush(t *testing.T) {
	store := &policyCaptureStore{}
	handler := NewInternalPolicyHandlerWithSizeLimit(store, 10*1024*1024)
	request := httptest.NewRequest(http.MethodPost, "/internal/lore/policy", strings.NewReader(`{
		"userId":"user-a",
		"resourceId":"urc-0123456789abcdef0123456789abcdef",
		"operation":"branch_push",
		"revision_tree_size":15728640
	}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("oversized push status = %d, body = %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, `"code":"repository_size_limit"`) &&
		!strings.Contains(body, `"code": "repository_size_limit"`) {
		t.Fatalf("oversized push body missing code: %s", body)
	}
	if !strings.Contains(body, "10.0 MB") || !strings.Contains(body, "15.0 MB") {
		t.Fatalf("oversized push body missing sizes: %s", body)
	}
}

func TestInternalPolicyHandlerAllowsBranchPushWithinSizeLimit(t *testing.T) {
	store := &policyCaptureStore{}
	handler := NewInternalPolicyHandlerWithSizeLimit(store, 10*1024*1024)
	request := httptest.NewRequest(http.MethodPost, "/internal/lore/policy", strings.NewReader(`{
		"userId":"user-a",
		"resourceId":"urc-0123456789abcdef0123456789abcdef",
		"operation":"branch_push",
		"revision_tree_size":10485760
	}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("in-limit push status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestInternalPolicyHandlerAllowsBranchPushWithoutRevisionTreeSize(t *testing.T) {
	store := &policyCaptureStore{}
	handler := NewInternalPolicyHandlerWithSizeLimit(store, 10*1024*1024)
	request := httptest.NewRequest(http.MethodPost, "/internal/lore/policy", strings.NewReader(`{
		"userId":"user-a",
		"resourceId":"urc-0123456789abcdef0123456789abcdef",
		"operation":"branch_push"
	}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("missing tree size status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestInternalPolicyHandlerIgnoresRevisionTreeSizeForOtherHookPoints(t *testing.T) {
	store := &policyCaptureStore{}
	handler := NewInternalPolicyHandlerWithSizeLimit(store, 1)
	request := httptest.NewRequest(http.MethodPost, "/internal/lore/policy", strings.NewReader(`{
		"userId":"user-a",
		"resourceId":"urc-0123456789abcdef0123456789abcdef",
		"operation":"branch_create",
		"branchId":"branch-id",
		"branchName":"feature/size",
		"revision_tree_size":999999
	}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("branch create status = %d, body = %s", response.Code, response.Body.String())
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

func TestPerServerPolicyCertificateAcceptsAssignedRepository(t *testing.T) {
	serverID := "c727d690-34d4-4b44-bd13-a132f89c5919"
	repositoryID := "0123456789abcdef0123456789abcdef"
	store := &policyHookCaptureStore{
		policyCaptureStore: &policyCaptureStore{},
		serverID:           serverID, ownedRepository: repositoryID, active: true,
	}
	handler := NewInternalPolicyHandler(store)
	request := policyHookRequest(
		http.MethodPost, "/internal/lore/policy", "lore-server-"+serverID,
		`{"userId":"user-a","resourceId":"urc-`+repositoryID+`","operation":"branch_push"}`,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("policy response status = %d, body = %s", response.Code, response.Body.String())
	}
	if store.contextServerID != serverID {
		t.Fatalf("request context server ID = %q", store.contextServerID)
	}
}

func TestPerServerPolicyCertificateRejectsAnotherServersRepository(t *testing.T) {
	serverID := "c727d690-34d4-4b44-bd13-a132f89c5919"
	store := &policyHookCaptureStore{
		policyCaptureStore: &policyCaptureStore{},
		serverID:           serverID, ownedRepository: "0123456789abcdef0123456789abcdef", active: true,
	}
	handler := NewInternalPolicyHandler(store)
	request := policyHookRequest(
		http.MethodPost, "/internal/lore/policy", "lore-server-"+serverID,
		`{"userId":"user-a","resourceId":"urc-fedcba9876543210fedcba9876543210","operation":"branch_push"}`,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("policy response status = %d, body = %s", response.Code, response.Body.String())
	}
	if store.check.ResourceID != "" {
		t.Fatalf("policy evaluation ran for another server's repository: %+v", store.check)
	}
}

func TestPerServerPolicyCertificateRejectsAnotherServersObservation(t *testing.T) {
	serverID := "c727d690-34d4-4b44-bd13-a132f89c5919"
	store := &policyHookCaptureStore{
		policyCaptureStore: &policyCaptureStore{},
		serverID:           serverID, ownedRepository: "0123456789abcdef0123456789abcdef", active: true,
	}
	handler := NewInternalPolicyHandler(store)
	request := policyHookRequest(
		http.MethodPost, "/internal/lore/observation", "lore-server-"+serverID,
		`{"userId":"user-a","resourceId":"urc-fedcba9876543210fedcba9876543210",`+
			`"operation":"branch_push","branchId":"branch-a","revision":"revision-a"}`,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("observation response status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestLegacyPolicyCertificateRemainsUnrestricted(t *testing.T) {
	store := &policyHookCaptureStore{
		policyCaptureStore: &policyCaptureStore{},
		serverID:           "c727d690-34d4-4b44-bd13-a132f89c5919",
	}
	handler := NewInternalPolicyHandler(store)
	request := policyHookRequest(
		http.MethodPost, "/internal/lore/policy", servercert.LegacyCommonName,
		`{"userId":"user-a","resourceId":"urc-fedcba9876543210fedcba9876543210","operation":"branch_push"}`,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("legacy policy response status = %d, body = %s", response.Code, response.Body.String())
	}
	if store.activeLookups != 0 {
		t.Fatalf("legacy certificate performed %d server lookups", store.activeLookups)
	}
}

func TestRevokedLoreServerCertificateIsRejected(t *testing.T) {
	serverID := "c727d690-34d4-4b44-bd13-a132f89c5919"
	store := &policyHookCaptureStore{
		policyCaptureStore: &policyCaptureStore{}, serverID: serverID, active: false,
	}
	handler := NewInternalPolicyHandler(store)
	request := policyHookRequest(
		http.MethodPost, "/internal/lore/policy", "lore-server-"+serverID,
		`{"userId":"user-a","resourceId":"urc-0123456789abcdef0123456789abcdef","operation":"branch_push"}`,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("revoked server response status = %d, body = %s", response.Code, response.Body.String())
	}
	if store.check.ResourceID != "" {
		t.Fatalf("policy evaluation ran for a revoked server: %+v", store.check)
	}
}

func policyHookRequest(method string, path string, commonName string, body string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{
		Subject: pkix.Name{CommonName: commonName},
	}}}
	return request
}
