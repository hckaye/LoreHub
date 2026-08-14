package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type instanceAdminActorContextKey struct{}

type entitlementRequest struct {
	OrganizationID string `json:"organizationId"`
	UserID         string `json:"userId"`
	Feature        string `json:"feature"`
}

func (api *API) requireInstanceAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !api.instanceAdminEnabled {
			http.NotFound(writer, request)
			return
		}
		actor, ok := api.actor(writer, request)
		if !ok {
			return
		}
		username := strings.ToLower(strings.TrimSpace(actor.Username))
		if _, allowed := api.instanceAdminUsernames[username]; !allowed {
			writeProblem(writer, http.StatusForbidden, "forbidden", "This operation is not permitted")
			return
		}
		ctx := context.WithValue(request.Context(), instanceAdminActorContextKey{}, actor)
		next.ServeHTTP(writer, request.WithContext(ctx))
	})
}

func (api *API) listEntitlements(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "private, no-store")
	if api.entitlements == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "entitlements_unavailable", "Entitlements are unavailable")
		return
	}
	entitlements, err := api.entitlements.List(request.Context())
	if err != nil {
		api.internalError(writer, request, "list entitlements", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"entitlements": entitlements})
}

func (api *API) grantEntitlement(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "private, no-store")
	if api.entitlements == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "entitlements_unavailable", "Entitlements are unavailable")
		return
	}
	input, subject, ok := decodeEntitlementRequest(writer, request)
	if !ok {
		return
	}
	entitlement, err := api.entitlements.Grant(
		request.Context(), instanceAdminActor(request), subject, input.Feature,
	)
	if err != nil {
		api.platformError(writer, request, "grant entitlement", err)
		return
	}
	writeJSON(writer, http.StatusCreated, entitlement)
}

func (api *API) revokeEntitlement(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "private, no-store")
	if api.entitlements == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "entitlements_unavailable", "Entitlements are unavailable")
		return
	}
	input, subject, ok := decodeEntitlementRequest(writer, request)
	if !ok {
		return
	}
	if err := api.entitlements.Revoke(
		request.Context(), instanceAdminActor(request), subject, input.Feature,
	); err != nil {
		api.platformError(writer, request, "revoke entitlement", err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func decodeEntitlementRequest(
	writer http.ResponseWriter,
	request *http.Request,
) (entitlementRequest, platform.EntitlementSubject, bool) {
	var input entitlementRequest
	if !decodeJSON(writer, request, &input) {
		return entitlementRequest{}, platform.EntitlementSubject{}, false
	}
	input.OrganizationID = strings.TrimSpace(input.OrganizationID)
	input.UserID = strings.TrimSpace(input.UserID)
	input.Feature = strings.TrimSpace(input.Feature)
	if (input.OrganizationID == "") == (input.UserID == "") ||
		!validEntitlementSubjectID(input.OrganizationID, input.UserID) ||
		(input.Feature != platform.EntitlementHostedLoreServer &&
			input.Feature != platform.EntitlementHostedRunners) {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "Entitlement fields are invalid")
		return entitlementRequest{}, platform.EntitlementSubject{}, false
	}
	return input, platform.EntitlementSubject{
		OrganizationID: input.OrganizationID,
		UserID:         input.UserID,
	}, true
}

func validEntitlementSubjectID(organizationID string, userID string) bool {
	value := organizationID
	if value == "" {
		value = userID
	}
	_, err := uuid.Parse(value)
	return err == nil
}

func instanceAdminActor(request *http.Request) platform.User {
	actor, _ := request.Context().Value(instanceAdminActorContextKey{}).(platform.User)
	return actor
}
