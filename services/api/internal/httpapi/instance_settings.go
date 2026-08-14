package httpapi

import (
	"net/http"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type instanceSettingsResponse struct {
	HostedLoreServerEnabled  bool  `json:"hostedLoreServerEnabled"`
	HostedLoreServerOverride *bool `json:"hostedLoreServerOverride"`
	HostedLoreServerDefault  bool  `json:"hostedLoreServerDefault"`
}

type instanceSettingsRequest struct {
	HostedLoreServerOverride *bool `json:"hostedLoreServerOverride"`
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
	if err := api.instanceSettings.SetHostedLoreServerOverride(
		request.Context(), instanceAdminActor(request), input.HostedLoreServerOverride,
	); err != nil {
		api.platformError(writer, request, "update instance settings", err)
		return
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
	override, err := api.instanceSettings.GetHostedLoreServerOverride(request.Context())
	if err != nil {
		api.internalError(writer, request, "read instance settings", err)
		return instanceSettingsResponse{}, false
	}
	enabled := api.hostedLoreServerDefault
	if override != nil {
		enabled = *override
	}
	return instanceSettingsResponse{
		HostedLoreServerEnabled:  enabled,
		HostedLoreServerOverride: override,
		HostedLoreServerDefault:  api.hostedLoreServerDefault,
	}, true
}

var _ InstanceSettingsStore = (*platform.Store)(nil)
