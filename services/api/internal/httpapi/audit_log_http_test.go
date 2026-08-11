package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/auth"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type auditLogHTTPStore struct {
	IdentityStore
	page   platform.AuditLogPage
	err    error
	query  string
	cursor string
	limit  int
}

func (store *auditLogHTTPStore) OrganizationAuditLog(
	_ context.Context,
	_ platform.User,
	_ string,
	query string,
	cursor string,
	limit int,
) (platform.AuditLogPage, error) {
	store.query = query
	store.cursor = cursor
	store.limit = limit
	return store.page, store.err
}

func TestOrganizationAuditLogHTTPValidatesAndPreservesAuthorizationErrors(t *testing.T) {
	codec, err := auth.NewSecretCodec("audit log HTTP test secret")
	if err != nil {
		t.Fatal(err)
	}
	authenticationStore := &fakeAuthenticationStore{}
	newHandler := func(store *auditLogHTTPStore) http.Handler {
		return New(
			fakeStore{user: platform.User{ID: "user-1", Username: "alice"}},
			fakeLore{}, auth.DisabledAuthenticator{}, healthy{}, "",
			slog.New(slog.NewTextHandler(io.Discard, nil)),
			WithAuthentication(AuthOptions{
				SessionStore: authenticationStore,
				Secrets:      codec,
				PublicOrigin: "https://app.example",
				SessionCookie: SessionCookieOptions{
					Name: "lorehub_session", Path: "/", Secure: true,
				},
			}),
			WithIdentityStore(store),
		)
	}
	requestFor := func(store *auditLogHTTPStore, path string) *httptest.ResponseRecorder {
		cookie, _ := prepareSessionCookie(t, authenticationStore, codec)
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		newHandler(store).ServeHTTP(response, request)
		return response
	}

	store := &auditLogHTTPStore{page: platform.AuditLogPage{Items: []platform.AuditEvent{}}}
	response := requestFor(store, "/api/v1/organizations/acme/audit-log?query=team&before=cursor&perPage=25")
	if response.Code != http.StatusOK || store.query != "team" || store.cursor != "cursor" || store.limit != 25 {
		t.Fatalf("audit response=%d query=%q cursor=%q limit=%d", response.Code, store.query, store.cursor,
			store.limit)
	}
	if response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("audit cache policy = %q", response.Header().Get("Cache-Control"))
	}

	store = &auditLogHTTPStore{err: platform.ErrForbidden}
	response = requestFor(store, "/api/v1/organizations/acme/audit-log")
	if response.Code != http.StatusForbidden {
		t.Fatalf("forbidden audit response = %d %s", response.Code, response.Body.String())
	}
	store = &auditLogHTTPStore{err: platform.ErrInvalidAuditCursor}
	response = requestFor(store, "/api/v1/organizations/acme/audit-log?before=bad")
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "invalid_cursor") {
		t.Fatalf("invalid cursor response = %d %s", response.Code, response.Body.String())
	}
	store = &auditLogHTTPStore{}
	response = requestFor(store, "/api/v1/organizations/acme/audit-log?query=line%0Abreak")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("control character response = %d %s", response.Code, response.Body.String())
	}
	response = requestFor(store, "/api/v1/organizations/acme/audit-log?query=one&query=two")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("duplicate query response = %d %s", response.Code, response.Body.String())
	}
}
