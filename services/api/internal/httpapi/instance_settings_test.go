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
	hostedOverride *bool
	orgOverride    *int64
	repoOverride   *int64
	sizeOverride   *int64
	actor          platform.User
	setCalls       int
	setErrors      error
}

func (store *instanceSettingsHTTPStore) GetHostedLoreServerOverride(context.Context) (*bool, error) {
	if store.hostedOverride == nil {
		return nil, nil
	}
	value := *store.hostedOverride
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
		store.hostedOverride = nil
		return nil
	}
	copy := *value
	store.hostedOverride = &copy
	return nil
}

func (store *instanceSettingsHTTPStore) GetMaxOrganizationsPerUserOverride(context.Context) (*int64, error) {
	return cloneInt64(store.orgOverride), nil
}

func (store *instanceSettingsHTTPStore) SetMaxOrganizationsPerUserOverride(
	_ context.Context,
	actor platform.User,
	value *int64,
) error {
	return store.setInt64Override(actor, value, &store.orgOverride)
}

func (store *instanceSettingsHTTPStore) GetMaxRepositoriesPerOrganizationOverride(context.Context) (*int64, error) {
	return cloneInt64(store.repoOverride), nil
}

func (store *instanceSettingsHTTPStore) SetMaxRepositoriesPerOrganizationOverride(
	_ context.Context,
	actor platform.User,
	value *int64,
) error {
	return store.setInt64Override(actor, value, &store.repoOverride)
}

func (store *instanceSettingsHTTPStore) GetMaxRepositorySizeBytesOverride(context.Context) (*int64, error) {
	return cloneInt64(store.sizeOverride), nil
}

func (store *instanceSettingsHTTPStore) SetMaxRepositorySizeBytesOverride(
	_ context.Context,
	actor platform.User,
	value *int64,
) error {
	return store.setInt64Override(actor, value, &store.sizeOverride)
}

func (store *instanceSettingsHTTPStore) setInt64Override(
	actor platform.User,
	value *int64,
	target **int64,
) error {
	store.setCalls++
	store.actor = actor
	if store.setErrors != nil {
		return store.setErrors
	}
	*target = cloneInt64(value)
	return nil
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
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
	if initial.MaxOrganizationsPerUser != 0 || initial.MaxOrganizationsPerUserOverride != nil ||
		initial.MaxRepositoriesPerOrganization != 0 || initial.MaxRepositoriesPerOrganizationOverride != nil ||
		initial.MaxRepositorySizeBytes != 0 || initial.MaxRepositorySizeBytesOverride != nil {
		t.Fatalf("initial resource limits = %+v", initial)
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

func TestInstanceSettingsAPIResourceLimits(t *testing.T) {
	codec, err := auth.NewSecretCodec("instance settings resource limits secret")
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
		WithResourceLimitDefaults(3, 5, 10485760),
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
	if initial.MaxOrganizationsPerUser != 3 || initial.MaxOrganizationsPerUserOverride != nil ||
		initial.MaxOrganizationsPerUserDefault != 3 ||
		initial.MaxRepositoriesPerOrganization != 5 || initial.MaxRepositoriesPerOrganizationOverride != nil ||
		initial.MaxRepositoriesPerOrganizationDefault != 5 ||
		initial.MaxRepositorySizeBytes != 10485760 || initial.MaxRepositorySizeBytesOverride != nil ||
		initial.MaxRepositorySizeBytesDefault != 10485760 {
		t.Fatalf("default resource limits = %+v", initial)
	}

	putRequest := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/settings",
		bytes.NewBufferString(`{
			"maxOrganizationsPerUserOverride":1,
			"maxRepositoriesPerOrganizationOverride":2,
			"maxRepositorySizeBytesOverride":1048576
		}`),
	)
	putRequest.AddCookie(cookie)
	putRequest.Header.Set("X-CSRF-Token", csrf)
	putResponse := httptest.NewRecorder()
	handler.ServeHTTP(putResponse, putRequest)
	if putResponse.Code != http.StatusOK || settingsStore.setCalls != 3 {
		t.Fatalf("PUT resource limits: status=%d calls=%d body=%s",
			putResponse.Code, settingsStore.setCalls, putResponse.Body.String())
	}
	var overridden instanceSettingsResponse
	if err := json.NewDecoder(putResponse.Body).Decode(&overridden); err != nil {
		t.Fatal(err)
	}
	if overridden.MaxOrganizationsPerUser != 1 || overridden.MaxOrganizationsPerUserOverride == nil ||
		*overridden.MaxOrganizationsPerUserOverride != 1 || overridden.MaxOrganizationsPerUserDefault != 3 ||
		overridden.MaxRepositoriesPerOrganization != 2 || overridden.MaxRepositoriesPerOrganizationOverride == nil ||
		*overridden.MaxRepositoriesPerOrganizationOverride != 2 ||
		overridden.MaxRepositoriesPerOrganizationDefault != 5 ||
		overridden.MaxRepositorySizeBytes != 1048576 || overridden.MaxRepositorySizeBytesOverride == nil ||
		*overridden.MaxRepositorySizeBytesOverride != 1048576 ||
		overridden.MaxRepositorySizeBytesDefault != 10485760 {
		t.Fatalf("overridden resource limits = %+v", overridden)
	}
	if overridden.HostedLoreServerOverride != nil {
		t.Fatalf("absent hosted Lore server field changed override: %+v", overridden)
	}

	negative := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/settings",
		bytes.NewBufferString(`{"maxOrganizationsPerUserOverride":-1}`),
	)
	negative.AddCookie(cookie)
	negative.Header.Set("X-CSRF-Token", csrf)
	negativeResponse := httptest.NewRecorder()
	handler.ServeHTTP(negativeResponse, negative)
	if negativeResponse.Code != http.StatusBadRequest || settingsStore.setCalls != 3 {
		t.Fatalf("negative PUT: status=%d calls=%d body=%s",
			negativeResponse.Code, settingsStore.setCalls, negativeResponse.Body.String())
	}

	hostedOnly := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/settings",
		bytes.NewBufferString(`{"hostedLoreServerOverride":true}`),
	)
	hostedOnly.AddCookie(cookie)
	hostedOnly.Header.Set("X-CSRF-Token", csrf)
	hostedOnlyResponse := httptest.NewRecorder()
	handler.ServeHTTP(hostedOnlyResponse, hostedOnly)
	if hostedOnlyResponse.Code != http.StatusOK || settingsStore.setCalls != 4 {
		t.Fatalf("hosted-only PUT: status=%d calls=%d body=%s",
			hostedOnlyResponse.Code, settingsStore.setCalls, hostedOnlyResponse.Body.String())
	}
	var hosted instanceSettingsResponse
	if err := json.NewDecoder(hostedOnlyResponse.Body).Decode(&hosted); err != nil {
		t.Fatal(err)
	}
	if hosted.MaxOrganizationsPerUserOverride == nil || *hosted.MaxOrganizationsPerUserOverride != 1 ||
		hosted.HostedLoreServerOverride == nil || !*hosted.HostedLoreServerOverride {
		t.Fatalf("absent resource limit fields changed overrides: %+v", hosted)
	}

	clearRequest := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/admin/settings",
		bytes.NewBufferString(`{
			"maxOrganizationsPerUserOverride":null,
			"maxRepositoriesPerOrganizationOverride":null,
			"maxRepositorySizeBytesOverride":null
		}`),
	)
	clearRequest.AddCookie(cookie)
	clearRequest.Header.Set("X-CSRF-Token", csrf)
	clearResponse := httptest.NewRecorder()
	handler.ServeHTTP(clearResponse, clearRequest)
	if clearResponse.Code != http.StatusOK || settingsStore.setCalls != 7 {
		t.Fatalf("clear resource limits: status=%d calls=%d body=%s",
			clearResponse.Code, settingsStore.setCalls, clearResponse.Body.String())
	}
	var cleared instanceSettingsResponse
	if err := json.NewDecoder(clearResponse.Body).Decode(&cleared); err != nil {
		t.Fatal(err)
	}
	if cleared.MaxOrganizationsPerUser != 3 || cleared.MaxOrganizationsPerUserOverride != nil ||
		cleared.MaxRepositoriesPerOrganization != 5 || cleared.MaxRepositoriesPerOrganizationOverride != nil ||
		cleared.MaxRepositorySizeBytes != 10485760 || cleared.MaxRepositorySizeBytesOverride != nil {
		t.Fatalf("cleared resource limits = %+v", cleared)
	}
	if cleared.HostedLoreServerOverride == nil || !*cleared.HostedLoreServerOverride {
		t.Fatalf("clearing resource limits changed hosted Lore server override: %+v", cleared)
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
