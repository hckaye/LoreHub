package httpapi

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/auth"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type entitlementHTTPStore struct {
	entitlements []platform.Entitlement
	actor        platform.User
	subject      platform.EntitlementSubject
	feature      string
	listCalls    int
	grantCalls   int
	revokeCalls  int
	err          error
}

func (store *entitlementHTTPStore) Grant(
	_ context.Context,
	actor platform.User,
	subject platform.EntitlementSubject,
	feature string,
) (platform.Entitlement, error) {
	store.actor = actor
	store.subject = subject
	store.feature = feature
	store.grantCalls++
	return platform.Entitlement{
		OrganizationID: stringPointer(subject.OrganizationID),
		UserID:         stringPointer(subject.UserID),
		Feature:        feature,
		GrantedBy:      stringPointer(actor.ID),
		GrantSource:    "admin",
		CreatedAt:      time.Now().UTC(),
	}, store.err
}

func (store *entitlementHTTPStore) Revoke(
	_ context.Context,
	actor platform.User,
	subject platform.EntitlementSubject,
	feature string,
) error {
	store.actor = actor
	store.subject = subject
	store.feature = feature
	store.revokeCalls++
	return store.err
}

func (store *entitlementHTTPStore) List(context.Context) ([]platform.Entitlement, error) {
	store.listCalls++
	return append([]platform.Entitlement(nil), store.entitlements...), store.err
}

func TestInstanceAdministratorEntitlementAPIWithSession(t *testing.T) {
	codec, err := auth.NewSecretCodec("entitlement HTTP test secret")
	if err != nil {
		t.Fatal(err)
	}
	authenticationStore := &fakeAuthenticationStore{}
	cookie, csrf := prepareSessionCookie(t, authenticationStore, codec)
	store := &entitlementHTTPStore{entitlements: []platform.Entitlement{{
		Feature:     platform.EntitlementHostedRunners,
		GrantSource: "migration",
		CreatedAt:   time.Now().UTC(),
	}}}
	handler := entitlementTestHandler(
		store,
		fakeStore{user: platform.User{ID: "user-1", Username: "alice"}},
		auth.DisabledAuthenticator{},
		WithAuthentication(AuthOptions{
			SessionStore: authenticationStore,
			Secrets:      codec,
			PublicOrigin: "https://app.example",
			SessionCookie: SessionCookieOptions{
				Name: "lorehub_session", Path: "/", Secure: true,
			},
		}),
		WithInstanceAdminUsernames([]string{"Alice"}),
	)

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/entitlements", nil)
	listRequest.AddCookie(cookie)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || store.listCalls != 1 ||
		listResponse.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("list response: status=%d calls=%d body=%s", listResponse.Code, store.listCalls,
			listResponse.Body.String())
	}

	organizationID := "11111111-1111-4111-8111-111111111111"
	withoutCSRF := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/entitlements",
		bytes.NewBufferString(`{"organizationId":"`+organizationID+`","feature":"hosted_runners"}`),
	)
	withoutCSRF.AddCookie(cookie)
	withoutCSRFResponse := httptest.NewRecorder()
	handler.ServeHTTP(withoutCSRFResponse, withoutCSRF)
	if withoutCSRFResponse.Code != http.StatusForbidden || store.grantCalls != 0 {
		t.Fatalf("grant without CSRF: status=%d calls=%d body=%s",
			withoutCSRFResponse.Code, store.grantCalls, withoutCSRFResponse.Body.String())
	}

	grantRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/entitlements",
		bytes.NewBufferString(`{"organizationId":"`+organizationID+`","feature":"hosted_runners"}`),
	)
	grantRequest.AddCookie(cookie)
	grantRequest.Header.Set("X-CSRF-Token", csrf)
	grantResponse := httptest.NewRecorder()
	handler.ServeHTTP(grantResponse, grantRequest)
	if grantResponse.Code != http.StatusCreated || store.grantCalls != 1 || store.actor.Username != "alice" ||
		store.subject.OrganizationID != organizationID || store.feature != platform.EntitlementHostedRunners {
		t.Fatalf("grant response: status=%d actor=%+v subject=%+v feature=%q body=%s",
			grantResponse.Code, store.actor, store.subject, store.feature, grantResponse.Body.String())
	}

	userID := "22222222-2222-4222-8222-222222222222"
	revokeRequest := httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/admin/entitlements",
		bytes.NewBufferString(`{"userId":"`+userID+`","feature":"hosted_lore_server"}`),
	)
	revokeRequest.AddCookie(cookie)
	revokeRequest.Header.Set("X-CSRF-Token", csrf)
	revokeResponse := httptest.NewRecorder()
	handler.ServeHTTP(revokeResponse, revokeRequest)
	if revokeResponse.Code != http.StatusNoContent || store.revokeCalls != 1 || store.subject.UserID != userID ||
		store.feature != platform.EntitlementHostedLoreServer {
		t.Fatalf("revoke response: status=%d subject=%+v feature=%q body=%s",
			revokeResponse.Code, store.subject, store.feature, revokeResponse.Body.String())
	}
}

func TestInstanceAdministratorGuardRejectsUnconfiguredUser(t *testing.T) {
	codec, err := auth.NewSecretCodec("entitlement guard test secret")
	if err != nil {
		t.Fatal(err)
	}
	authenticationStore := &fakeAuthenticationStore{}
	cookie, _ := prepareSessionCookie(t, authenticationStore, codec)
	store := &entitlementHTTPStore{}
	handler := entitlementTestHandler(
		store,
		fakeStore{user: platform.User{ID: "user-1", Username: "alice"}},
		auth.DisabledAuthenticator{},
		WithAuthentication(AuthOptions{
			SessionStore:  authenticationStore,
			Secrets:       codec,
			SessionCookie: SessionCookieOptions{Name: "lorehub_session", Path: "/"},
		}),
		WithInstanceAdminUsernames([]string{"bob"}),
	)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/entitlements", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || store.listCalls != 0 {
		t.Fatalf("guard response: status=%d calls=%d body=%s", response.Code, store.listCalls,
			response.Body.String())
	}
}

func TestInstanceAdministratorGuardUsesResolvedPATUser(t *testing.T) {
	store := &entitlementHTTPStore{}
	handler := entitlementTestHandler(
		store,
		fakeStore{user: platform.User{ID: "user-1", Username: "alice"}},
		staticAuthenticator{principal: auth.Principal{
			InternalUserID: "user-1",
			Username:       "untrusted-token-username",
			CredentialKind: auth.CredentialPersonalAccessToken,
			CredentialID:   "token-1",
			Scopes:         []string{auth.ScopeReadAPI},
		}},
		WithInstanceAdminUsernames([]string{"alice"}),
	)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/entitlements", nil)
	request.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || store.listCalls != 1 {
		t.Fatalf("PAT guard response: status=%d calls=%d body=%s", response.Code, store.listCalls,
			response.Body.String())
	}
}

func entitlementTestHandler(
	store *entitlementHTTPStore,
	users Store,
	authenticator auth.Authenticator,
	options ...Option,
) http.Handler {
	options = append(options, WithEntitlements(store))
	return New(
		users,
		fakeLore{},
		authenticator,
		healthy{},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		options...,
	)
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
