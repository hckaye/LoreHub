package collab

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func TestListLabelsReturnsViewerWriteCapability(t *testing.T) {
	permissionChecks := 0
	store := &fakeStore{
		user: platform.User{ID: "user-1"},
		lookupRepo: func(_ *platform.User, _, _ string) (Repository, error) {
			return Repository{ID: "repo-1"}, nil
		},
		permission: func(_ platform.User, _ Repository) (Access, error) {
			permissionChecks++
			return Access{Permission: PermWrite}, nil
		},
		listLabels: func(_ string, _ Page) (Result[Label], error) {
			return Result[Label]{Items: []Label{{ID: "label-1", Name: "bug"}}}, nil
		},
	}
	handler := newTestAPI(store)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/repositories/acme/lore/labels", nil)
	request.Header.Set("Authorization", "Bearer user-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || permissionChecks != 1 {
		t.Fatalf("authenticated label list status=%d permission checks=%d", response.Code, permissionChecks)
	}
	var body labelListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.ViewerCanWrite || len(body.Items) != 1 || body.Items[0].ID != "label-1" {
		t.Fatalf("authenticated label list = %+v", body)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/repositories/acme/lore/labels", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || permissionChecks != 1 {
		t.Fatalf("anonymous label list status=%d permission checks=%d", response.Code, permissionChecks)
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.ViewerCanWrite {
		t.Fatal("anonymous label list reported write permission")
	}
}
