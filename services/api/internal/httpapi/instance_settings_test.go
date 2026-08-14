package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/auth"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type instanceSettingsHTTPStore struct {
	override  *bool
	actor     platform.User
	setCalls  int
	setErrors error
}

func (store *instanceSettingsHTTPStore) GetHostedLoreServerOverride(context.Context) (*bool, error) {
	if store.override == nil {
		return nil, nil
	}
	value := *store.override
	return &value, nil
}

func (store *instanceSettingsHTTPStore) SetHostedLoreServerOverride(
	_ context.Context,
	actor platform.User,
	value *bool,
) error {
	store.setCalls++
	store.actor = actor
	if store.setErrors != nil {
		return store.setErrors
	}
	if value == nil {
		store.override = nil
		return nil
	}
	copy := *value
	store.override = &copy
	return nil
}

func TestInstanceSettingsAPIWithSession(t *testing.T) {
	codec, err := auth.NewSecretCodec("instance settings HTTP test secret")
	if err != nil {
		t.Fatal(err)
	}
	authenticationStore := &fakeAuthenticationStore{}
	cookie, csrf := prepareSessionCookie(t, authenticationStore, codec)
	settingsStore := &instanceSettingsHTTPStore{}
	handler := instanceSettingsTestHandler(
		authenticationStore,
		codec,
		settingsStore,
		WithInstanceAdminUsernames([]string{"Alice"}),
		WithInstanceSettings(settingsStore, false),
	)

	getRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	getRequest.AddCookie(cookie)
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("GET settings status = %d, body=%s", getResponse.Code, getResponse.Body.String())
	}
	var initial instanceSettingsResponse
	if err := json.NewDecoder(getResponse.Body).Decode(&initial); err != nil {
		t.Fatal(err)
	}
	if initial.HostedLoreServerEnabled || initial.HostedLoreServerOverride != nil ||
		initial.HostedLoreServerDefault {
		t.Fatalf("initial settings = %+v", initial)
	}

	withoutCSRF := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/settings",
		bytes.NewBufferString(`{"hostedLoreServerOverride":true}`),
	)
	withoutCSRF.AddCookie(cookie)
	withoutCSRFResponse := httptest.NewRecorder()
	handler.ServeHTTP(withoutCSRFResponse, withoutCSRF)
	if withoutCSRFResponse.Code != http.StatusForbidden || settingsStore.setCalls != 0 {
		t.Fatalf("PUT without CSRF: status=%d calls=%d body=%s",
			withoutCSRFResponse.Code, settingsStore.setCalls, withoutCSRFResponse.Body.String())
	}

	putRequest := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/settings",
		bytes.NewBufferString(`{"hostedLoreServerOverride":true}`),
	)
	putRequest.AddCookie(cookie)
	putRequest.Header.Set("X-CSRF-Token", csrf)
	putResponse := httptest.NewRecorder()
	handler.ServeHTTP(putResponse, putRequest)
	if putResponse.Code != http.StatusOK || settingsStore.setCalls != 1 ||
		settingsStore.actor.Username != "alice" {
		t.Fatalf("PUT settings: status=%d calls=%d actor=%+v body=%s",
			putResponse.Code, settingsStore.setCalls, settingsStore.actor, putResponse.Body.String())
	}
	var enabled instanceSettingsResponse
	if err := json.NewDecoder(putResponse.Body).Decode(&enabled); err != nil {
		t.Fatal(err)
	}
	if !enabled.HostedLoreServerEnabled || enabled.HostedLoreServerOverride == nil ||
		!*enabled.HostedLoreServerOverride || enabled.HostedLoreServerDefault {
		t.Fatalf("enabled settings = %+v", enabled)
	}

	malformed := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/settings",
		bytes.NewBufferString(`{"hostedLoreServerOverride":"yes"}`),
	)
	malformed.AddCookie(cookie)
	malformed.Header.Set("X-CSRF-Token", csrf)
	malformedResponse := httptest.NewRecorder()
	handler.ServeHTTP(malformedResponse, malformed)
	if malformedResponse.Code != http.StatusBadRequest || settingsStore.setCalls != 1 {
		t.Fatalf("malformed PUT: status=%d calls=%d body=%s",
			malformedResponse.Code, settingsStore.setCalls, malformedResponse.Body.String())
	}

	clearRequest := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/settings",
		bytes.NewBufferString(`{"hostedLoreServerOverride":null}`),
	)
	clearRequest.AddCookie(cookie)
	clearRequest.Header.Set("X-CSRF-Token", csrf)
	clearResponse := httptest.NewRecorder()
	handler.ServeHTTP(clearResponse, clearRequest)
	if clearResponse.Code != http.StatusOK || settingsStore.setCalls != 2 {
		t.Fatalf("clear settings: status=%d calls=%d body=%s",
			clearResponse.Code, settingsStore.setCalls, clearResponse.Body.String())
	}
	var cleared instanceSettingsResponse
	if err := json.NewDecoder(clearResponse.Body).Decode(&cleared); err != nil {
		t.Fatal(err)
	}
	if cleared.HostedLoreServerEnabled || cleared.HostedLoreServerOverride != nil {
		t.Fatalf("cleared settings = %+v", cleared)
	}
}

func TestInstanceSettingsAPINonAdminIsForbidden(t *testing.T) {
	codec, err := auth.NewSecretCodec("instance settings non-admin test secret")
	if err != nil {
		t.Fatal(err)
	}
	authenticationStore := &fakeAuthenticationStore{}
	cookie, _ := prepareSessionCookie(t, authenticationStore, codec)
	settingsStore := &instanceSettingsHTTPStore{}
	handler := instanceSettingsTestHandler(
		authenticationStore,
		codec,
		settingsStore,
		WithInstanceAdminUsernames([]string{"bob"}),
		WithInstanceSettings(settingsStore, true),
	)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || settingsStore.setCalls != 0 {
		t.Fatalf("non-admin response: status=%d calls=%d body=%s",
			response.Code, settingsStore.setCalls, response.Body.String())
	}
}

func TestInstanceAdminRoutesAreNotRegisteredWhenDisabled(t *testing.T) {
	settingsStore := &instanceSettingsHTTPStore{}
	handler := instanceSettingsTestHandler(
		nil,
		nil,
		settingsStore,
		WithInstanceAdminEnabled(false),
		WithInstanceAdminUsernames([]string{"alice"}),
		WithInstanceSettings(settingsStore, true),
	)
	for _, path := range []string{
		"/api/v1/admin/settings",
		"/api/v1/admin/entitlements",
		"/api/v1/admin/repositories/owner/repository/migrations",
	} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("disabled admin route %s status = %d, body=%s",
				path, response.Code, response.Body.String())
		}
	}
}

func instanceSettingsTestHandler(
	authenticationStore *fakeAuthenticationStore,
	codec *auth.SecretCodec,
	settingsStore *instanceSettingsHTTPStore,
	options ...Option,
) http.Handler {
	return New(
		fakeStore{user: platform.User{ID: "user-1", Username: "alice", DisplayName: "Alice"}},
		fakeLore{},
		auth.DisabledAuthenticator{},
		healthy{},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		append([]Option{
			WithAuthentication(AuthOptions{
				SessionStore: authenticationStore,
				Secrets:      codec,
			}),
		}, options...)...,
	)
}
