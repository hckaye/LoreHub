package webhooks

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

const maxRequestBytes = 1 << 20

type ActorResolver interface {
	ResolveActor(http.ResponseWriter, *http.Request) (platform.User, bool)
}

type Manager interface {
	List(context.Context, platform.User, string, string) ([]Webhook, error)
	Create(context.Context, platform.User, string, string, CreateInput) (Webhook, error)
	Update(context.Context, platform.User, string, string, string, UpdateInput) (Webhook, error)
	Delete(context.Context, platform.User, string, string, string) error
	ListDeliveries(context.Context, platform.User, string, string, string, int) ([]Delivery, error)
	DeliveryDetail(context.Context, platform.User, string, string, string, string) (DeliveryDetail, error)
	Redeliver(context.Context, platform.User, string, string, string, string) error
}

type API struct {
	store  Manager
	actors ActorResolver
	logger *slog.Logger
}

func Register(mux *http.ServeMux, store Manager, actors ActorResolver, logger *slog.Logger) {
	api := &API{store: store, actors: actors, logger: logger}
	base := "/api/v1/repositories/{owner}/{repository}/webhooks"
	mux.HandleFunc("GET "+base, api.list)
	mux.HandleFunc("POST "+base, api.create)
	mux.HandleFunc("PATCH "+base+"/{webhookID}", api.update)
	mux.HandleFunc("DELETE "+base+"/{webhookID}", api.delete)
	mux.HandleFunc("GET "+base+"/{webhookID}/deliveries", api.listDeliveries)
	mux.HandleFunc("GET "+base+"/{webhookID}/deliveries/{deliveryID}", api.deliveryDetail)
	mux.HandleFunc("POST "+base+"/{webhookID}/deliveries/{deliveryID}/redeliver", api.redeliver)
}

func (api *API) list(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actors.ResolveActor(writer, request)
	if !ok {
		return
	}
	items, err := api.store.List(request.Context(), actor, owner(request), repository(request))
	if err != nil {
		api.storeError(writer, request, "list repository webhooks", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"webhooks": items, "availableEvents": availableEvents()})
}

func (api *API) create(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actors.ResolveActor(writer, request)
	if !ok {
		return
	}
	var input CreateInput
	if !decodeJSON(writer, request, &input) {
		return
	}
	webhook, err := api.store.Create(request.Context(), actor, owner(request), repository(request), input)
	if err != nil {
		api.storeError(writer, request, "create repository webhook", err)
		return
	}
	writer.Header().Set("Location", request.URL.Path+"/"+webhook.ID)
	writeJSON(writer, http.StatusCreated, webhook)
}

func (api *API) update(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actors.ResolveActor(writer, request)
	if !ok {
		return
	}
	var input UpdateInput
	if !decodeJSON(writer, request, &input) {
		return
	}
	webhook, err := api.store.Update(
		request.Context(), actor, owner(request), repository(request), request.PathValue("webhookID"), input,
	)
	if err != nil {
		api.storeError(writer, request, "update repository webhook", err)
		return
	}
	writeJSON(writer, http.StatusOK, webhook)
}

func (api *API) delete(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actors.ResolveActor(writer, request)
	if !ok {
		return
	}
	err := api.store.Delete(
		request.Context(), actor, owner(request), repository(request), request.PathValue("webhookID"),
	)
	if err != nil {
		api.storeError(writer, request, "delete repository webhook", err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (api *API) listDeliveries(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actors.ResolveActor(writer, request)
	if !ok {
		return
	}
	limit := 30
	if value := request.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 100 {
			writeProblem(writer, http.StatusBadRequest, "invalid_input", "limit must be between 1 and 100")
			return
		}
		limit = parsed
	}
	deliveries, err := api.store.ListDeliveries(
		request.Context(), actor, owner(request), repository(request), request.PathValue("webhookID"), limit,
	)
	if err != nil {
		api.storeError(writer, request, "list webhook deliveries", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"deliveries": deliveries})
}

func (api *API) deliveryDetail(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actors.ResolveActor(writer, request)
	if !ok {
		return
	}
	detail, err := api.store.DeliveryDetail(
		request.Context(), actor, owner(request), repository(request),
		request.PathValue("webhookID"), request.PathValue("deliveryID"),
	)
	if err != nil {
		api.storeError(writer, request, "read webhook delivery", err)
		return
	}
	writeJSON(writer, http.StatusOK, detail)
}

func (api *API) redeliver(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actors.ResolveActor(writer, request)
	if !ok {
		return
	}
	err := api.store.Redeliver(
		request.Context(), actor, owner(request), repository(request),
		request.PathValue("webhookID"), request.PathValue("deliveryID"),
	)
	if err != nil {
		api.storeError(writer, request, "queue webhook redelivery", err)
		return
	}
	writer.WriteHeader(http.StatusAccepted)
}

func (api *API) storeError(
	writer http.ResponseWriter,
	request *http.Request,
	operation string,
	err error,
) {
	switch {
	case errors.Is(err, ErrInvalid):
		writeProblem(writer, http.StatusBadRequest, "invalid_input", err.Error())
	case errors.Is(err, ErrNotFound):
		writeProblem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
	case errors.Is(err, ErrForbidden):
		writeProblem(writer, http.StatusForbidden, "forbidden", "This operation is not permitted")
	case errors.Is(err, ErrConflict):
		writeProblem(writer, http.StatusConflict, "conflict", "The webhook conflicts with existing state")
	default:
		api.logger.Error(operation, "error", err, "method", request.Method, "path", request.URL.Path)
		writeProblem(writer, http.StatusInternalServerError, "internal_error", "The request could not be completed")
	}
}

func decodeJSON(writer http.ResponseWriter, request *http.Request, target any) bool {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeProblem(writer, http.StatusUnsupportedMediaType, "unsupported_media_type",
			"Content-Type must be application/json")
		return false
	}
	request.Body = http.MaxBytesReader(writer, request.Body, maxRequestBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeProblem(writer, http.StatusRequestEntityTooLarge, "payload_too_large", "Request body is too large")
			return false
		}
		writeProblem(writer, http.StatusBadRequest, "invalid_json", "Request body is invalid")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeProblem(writer, http.StatusBadRequest, "invalid_json", "Request body must contain one JSON value")
		return false
	}
	return true
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeProblem(writer http.ResponseWriter, status int, code string, detail string) {
	writeJSON(writer, status, map[string]any{"error": map[string]string{"code": code, "detail": detail}})
}

func availableEvents() []string {
	events := make([]string, 0, len(eventKinds))
	for event := range eventKinds {
		events = append(events, event)
	}
	result, _ := normalizeEvents(events)
	return result
}

func owner(request *http.Request) string {
	return request.PathValue("owner")
}

func repository(request *http.Request) string {
	return request.PathValue("repository")
}
