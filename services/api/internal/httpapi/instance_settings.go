package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type instanceSettingsResponse struct {
	HostedLoreServerEnabled                bool   `json:"hostedLoreServerEnabled"`
	HostedLoreServerOverride               *bool  `json:"hostedLoreServerOverride"`
	HostedLoreServerDefault                bool   `json:"hostedLoreServerDefault"`
	MaxOrganizationsPerUser                int64  `json:"maxOrganizationsPerUser"`
	MaxOrganizationsPerUserOverride        *int64 `json:"maxOrganizationsPerUserOverride"`
	MaxOrganizationsPerUserDefault         int64  `json:"maxOrganizationsPerUserDefault"`
	MaxRepositoriesPerOrganization         int64  `json:"maxRepositoriesPerOrganization"`
	MaxRepositoriesPerOrganizationOverride *int64 `json:"maxRepositoriesPerOrganizationOverride"`
	MaxRepositoriesPerOrganizationDefault  int64  `json:"maxRepositoriesPerOrganizationDefault"`
	MaxRepositorySizeBytes                 int64  `json:"maxRepositorySizeBytes"`
	MaxRepositorySizeBytesOverride         *int64 `json:"maxRepositorySizeBytesOverride"`
	MaxRepositorySizeBytesDefault          int64  `json:"maxRepositorySizeBytesDefault"`
}

type instanceSettingsRequest struct {
	HostedLoreServerOverride               json.RawMessage `json:"hostedLoreServerOverride"`
	MaxOrganizationsPerUserOverride        json.RawMessage `json:"maxOrganizationsPerUserOverride"`
	MaxRepositoriesPerOrganizationOverride json.RawMessage `json:"maxRepositoriesPerOrganizationOverride"`
	MaxRepositorySizeBytesOverride         json.RawMessage `json:"maxRepositorySizeBytesOverride"`
}

func (api *API) getInstanceSettings(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "private, no-store")
	response, ok := api.readInstanceSettings(writer, request)
	if !ok {
		return
	}
	writeJSON(writer, http.StatusOK, response)
}

func (api *API) updateInstanceSettings(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "private, no-store")
	if api.instanceSettings == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "instance_settings_unavailable",
			"Instance settings are unavailable")
		return
	}
	var input instanceSettingsRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	actor := instanceAdminActor(request)
	hosted, hostedPresent, ok := parseOptionalBoolOverride(
		writer, input.HostedLoreServerOverride, "hostedLoreServerOverride",
	)
	if !ok {
		return
	}
	organizations, organizationsPresent, ok := parseOptionalInt64Override(
		writer, input.MaxOrganizationsPerUserOverride, "maxOrganizationsPerUserOverride",
	)
	if !ok {
		return
	}
	repositories, repositoriesPresent, ok := parseOptionalInt64Override(
		writer, input.MaxRepositoriesPerOrganizationOverride, "maxRepositoriesPerOrganizationOverride",
	)
	if !ok {
		return
	}
	sizeBytes, sizePresent, ok := parseOptionalInt64Override(
		writer, input.MaxRepositorySizeBytesOverride, "maxRepositorySizeBytesOverride",
	)
	if !ok {
		return
	}
	if hostedPresent {
		if err := api.instanceSettings.SetHostedLoreServerOverride(
			request.Context(), actor, hosted,
		); err != nil {
			api.platformError(writer, request, "update instance settings", err)
			return
		}
	}
	if organizationsPresent {
		if err := api.instanceSettings.SetMaxOrganizationsPerUserOverride(
			request.Context(), actor, organizations,
		); err != nil {
			api.platformError(writer, request, "update instance settings", err)
			return
		}
	}
	if repositoriesPresent {
		if err := api.instanceSettings.SetMaxRepositoriesPerOrganizationOverride(
			request.Context(), actor, repositories,
		); err != nil {
			api.platformError(writer, request, "update instance settings", err)
			return
		}
	}
	if sizePresent {
		if err := api.instanceSettings.SetMaxRepositorySizeBytesOverride(
			request.Context(), actor, sizeBytes,
		); err != nil {
			api.platformError(writer, request, "update instance settings", err)
			return
		}
	}
	response, ok := api.readInstanceSettings(writer, request)
	if !ok {
		return
	}
	writeJSON(writer, http.StatusOK, response)
}

func (api *API) readInstanceSettings(
	writer http.ResponseWriter,
	request *http.Request,
) (instanceSettingsResponse, bool) {
	if api.instanceSettings == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "instance_settings_unavailable",
			"Instance settings are unavailable")
		return instanceSettingsResponse{}, false
	}
	hostedOverride, err := api.instanceSettings.GetHostedLoreServerOverride(request.Context())
	if err != nil {
		api.internalError(writer, request, "read instance settings", err)
		return instanceSettingsResponse{}, false
	}
	organizationsOverride, err := api.instanceSettings.GetMaxOrganizationsPerUserOverride(request.Context())
	if err != nil {
		api.internalError(writer, request, "read instance settings", err)
		return instanceSettingsResponse{}, false
	}
	repositoriesOverride, err := api.instanceSettings.GetMaxRepositoriesPerOrganizationOverride(request.Context())
	if err != nil {
		api.internalError(writer, request, "read instance settings", err)
		return instanceSettingsResponse{}, false
	}
	sizeOverride, err := api.instanceSettings.GetMaxRepositorySizeBytesOverride(request.Context())
	if err != nil {
		api.internalError(writer, request, "read instance settings", err)
		return instanceSettingsResponse{}, false
	}
	hostedEnabled := api.hostedLoreServerDefault
	if hostedOverride != nil {
		hostedEnabled = *hostedOverride
	}
	return instanceSettingsResponse{
		HostedLoreServerEnabled:         hostedEnabled,
		HostedLoreServerOverride:        hostedOverride,
		HostedLoreServerDefault:         api.hostedLoreServerDefault,
		MaxOrganizationsPerUser:         effectiveInt64(organizationsOverride, api.maxOrganizationsPerUserDefault),
		MaxOrganizationsPerUserOverride: organizationsOverride,
		MaxOrganizationsPerUserDefault:  api.maxOrganizationsPerUserDefault,
		MaxRepositoriesPerOrganization: effectiveInt64(
			repositoriesOverride, api.maxRepositoriesPerOrganizationDefault),
		MaxRepositoriesPerOrganizationOverride: repositoriesOverride,
		MaxRepositoriesPerOrganizationDefault:  api.maxRepositoriesPerOrganizationDefault,
		MaxRepositorySizeBytes:                 effectiveInt64(sizeOverride, api.maxRepositorySizeBytes),
		MaxRepositorySizeBytesOverride:         sizeOverride,
		MaxRepositorySizeBytesDefault:          api.maxRepositorySizeBytes,
	}, true
}

func parseOptionalBoolOverride(
	writer http.ResponseWriter,
	raw json.RawMessage,
	_ string,
) (*bool, bool, bool) {
	if len(raw) == 0 {
		return nil, false, true
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, true, true
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_json", "Request body is invalid")
		return nil, false, false
	}
	return &value, true, true
}

func parseOptionalInt64Override(
	writer http.ResponseWriter,
	raw json.RawMessage,
	field string,
) (*int64, bool, bool) {
	if len(raw) == 0 {
		return nil, false, true
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, true, true
	}
	var value int64
	if err := json.Unmarshal(raw, &value); err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", field+" must be an integer or null")
		return nil, false, false
	}
	if value < 0 {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", field+" must be non-negative")
		return nil, false, false
	}
	return &value, true, true
}

func effectiveInt64(override *int64, fallback int64) int64 {
	if override != nil {
		return *override
	}
	return fallback
}

var _ InstanceSettingsStore = (*platform.Store)(nil)
