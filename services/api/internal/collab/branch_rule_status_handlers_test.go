package collab

import (
	"net/http"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func TestCreateBranchRuleInvalidRequiredStatusChecks(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		permission: func(platform.User, Repository) (Access, error) {
			return Access{Permission: PermAdmin}, nil
		},
		createRule: func(platform.User, string, BranchRuleInput) (BranchRule, error) {
			t.Fatal("invalid branch rule reached the store")
			return BranchRule{}, nil
		},
	}
	handler := newTestAPI(store)
	body := `{"pattern":"main","requiredStatusChecks":["CI/Test","ci/test"]}`
	recorder := doRequest(handler, http.MethodPost,
		"/api/v1/repositories/acme/lore/branch-rules", body,
		"Authorization", "Bearer alice", "Content-Type", "application/json")
	if recorder.Code != http.StatusBadRequest || errorCode(t, recorder) != "invalid_input" {
		t.Fatalf("expected 400 invalid_input, got %d %s", recorder.Code, recorder.Body.String())
	}
}
