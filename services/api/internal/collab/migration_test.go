package collab

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func TestRequireMutationActorRejectsMigratingRepository(t *testing.T) {
	migratingAt := time.Now().UTC()
	store := &fakeStore{
		user: platform.User{ID: "user-1", Username: "alice"},
		lookupRepo: func(*platform.User, string, string) (Repository, error) {
			return Repository{ID: "repository-1", MigratingAt: &migratingAt}, nil
		},
	}
	api := NewAPI(store, testActorResolver{store: store}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/repositories/acme/lore/issues", nil)
	request.Header.Set("Authorization", "Bearer alice")
	response := httptest.NewRecorder()
	_, _, ok := api.requireMutationActor(response, request)
	if ok || response.Code != http.StatusForbidden {
		t.Fatalf("migrating mutation result: ok=%t status=%d body=%s", ok, response.Code,
			response.Body.String())
	}
	if !contains(response.Body.String(), "repository_read_only") {
		t.Fatalf("migrating mutation response = %s", response.Body.String())
	}
}

func contains(value, fragment string) bool {
	for index := 0; index+len(fragment) <= len(value); index++ {
		if value[index:index+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
