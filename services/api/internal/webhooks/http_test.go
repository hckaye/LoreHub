package webhooks

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type fakeActorResolver struct {
	actor platform.User
	ok    bool
	calls int
}

func (resolver *fakeActorResolver) ResolveActor(
	writer http.ResponseWriter,
	_ *http.Request,
) (platform.User, bool) {
	resolver.calls++
	if !resolver.ok {
		writeProblem(writer, http.StatusForbidden, "csrf_failed", "A valid CSRF token is required")
		return platform.User{}, false
	}
	return resolver.actor, true
}

type fakeManager struct {
	items       []Webhook
	createInput CreateInput
	err         error
	redelivered bool
}

func (manager *fakeManager) List(
	context.Context,
	platform.User,
	string,
	string,
) ([]Webhook, error) {
	return manager.items, manager.err
}

func (manager *fakeManager) Create(
	_ context.Context,
	_ platform.User,
	_ string,
	_ string,
	input CreateInput,
) (Webhook, error) {
	manager.createInput = input
	if manager.err != nil {
		return Webhook{}, manager.err
	}
	return Webhook{ID: "hook-id", URL: input.URL, Events: input.Events, SecretConfigured: true}, nil
}

func (manager *fakeManager) Update(
	context.Context,
	platform.User,
	string,
	string,
	string,
	UpdateInput,
) (Webhook, error) {
	return Webhook{}, manager.err
}

func (manager *fakeManager) Delete(
	context.Context,
	platform.User,
	string,
	string,
	string,
) error {
	return manager.err
}

func (manager *fakeManager) ListDeliveries(
	context.Context,
	platform.User,
	string,
	string,
	string,
	int,
) ([]Delivery, error) {
	return []Delivery{}, manager.err
}

func (manager *fakeManager) DeliveryDetail(
	context.Context,
	platform.User,
	string,
	string,
	string,
	string,
) (DeliveryDetail, error) {
	return DeliveryDetail{}, manager.err
}

func (manager *fakeManager) Redeliver(
	context.Context,
	platform.User,
	string,
	string,
	string,
	string,
) error {
	manager.redelivered = true
	return manager.err
}

func TestWebhookHTTPCreateUsesSharedActorAndHidesSecret(t *testing.T) {
	manager := &fakeManager{}
	actors := &fakeActorResolver{actor: platform.User{ID: "actor"}, ok: true}
	handler := webhookTestHandler(manager, actors)
	body := `{"url":"https://hooks.example/events","events":["issues"],` +
		`"secret":"0123456789abcdef"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/repositories/acme/game/webhooks", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || actors.calls != 1 {
		t.Fatalf("status = %d actor calls = %d body = %s", response.Code, actors.calls, response.Body.String())
	}
	if manager.createInput.Secret != "0123456789abcdef" ||
		strings.Contains(response.Body.String(), "0123456789abcdef") {
		t.Fatalf("secret handling failed: %s", response.Body.String())
	}
	if response.Header().Get("Location") != "/api/v1/repositories/acme/game/webhooks/hook-id" {
		t.Fatalf("location = %q", response.Header().Get("Location"))
	}
}

func TestWebhookHTTPRejectsUnknownJSONAndDelegatesCSRF(t *testing.T) {
	manager := &fakeManager{}
	actors := &fakeActorResolver{ok: true}
	handler := webhookTestHandler(manager, actors)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/repositories/acme/game/webhooks",
		strings.NewReader(`{"url":"https://hooks.example","unknown":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}

	actors.ok = false
	request = httptest.NewRequest(http.MethodDelete,
		"/api/v1/repositories/acme/game/webhooks/00000000-0000-4000-8000-000000000001", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || actors.calls != 2 {
		t.Fatalf("status = %d actor calls = %d", response.Code, actors.calls)
	}
}

func TestWebhookHTTPEnforcesJSONContentTypeAndBodyLimit(t *testing.T) {
	manager := &fakeManager{}
	actors := &fakeActorResolver{ok: true}
	handler := webhookTestHandler(manager, actors)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/repositories/acme/game/webhooks",
		strings.NewReader(`{"url":"https://hooks.example"}`))
	request.Header.Set("Content-Type", "text/plain")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("content type status = %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/repositories/acme/game/webhooks",
		strings.NewReader(`{"url":"`+strings.Repeat("a", maxRequestBytes)+`"}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("body limit status = %d body = %s", response.Code, response.Body.String())
	}
}

func TestWebhookHTTPMapsAuthorizationAndQueuesRedelivery(t *testing.T) {
	manager := &fakeManager{err: ErrForbidden}
	actors := &fakeActorResolver{actor: platform.User{ID: "actor"}, ok: true}
	handler := webhookTestHandler(manager, actors)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/repositories/acme/game/webhooks", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}

	manager.err = nil
	request = httptest.NewRequest(http.MethodPost,
		"/api/v1/repositories/acme/game/webhooks/hook/deliveries/delivery/redeliver", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || !manager.redelivered {
		t.Fatalf("status = %d redelivered = %t", response.Code, manager.redelivered)
	}
}

func webhookTestHandler(manager Manager, actors ActorResolver) http.Handler {
	mux := http.NewServeMux()
	Register(mux, manager, actors, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return mux
}
