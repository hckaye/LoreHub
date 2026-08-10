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
	check authz.PolicyCheck
}

func (store *policyCaptureStore) CheckPolicy(
	_ context.Context,
	check authz.PolicyCheck,
) (authz.PolicyDecision, error) {
	store.check = check
	return authz.PolicyDecision{Allowed: true}, nil
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
}
