package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/auth"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

const loreServerCredentialAge = 366 * 24 * time.Hour

func (api *API) createLoreServerRegistrationToken(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	if api.loreServers == nil || api.loresSecrets == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "lore_servers_unavailable",
			"Lore server settings are unavailable")
		return
	}
	expiresAt := time.Now().UTC().Add(time.Hour)
	if request.ContentLength != 0 {
		var input struct {
			ExpiresAt time.Time `json:"expiresAt"`
		}
		if !decodeJSON(writer, request, &input) {
			return
		}
		if !input.ExpiresAt.IsZero() {
			expiresAt = input.ExpiresAt
		}
	}
	raw, digest, err := auth.NewLoreServerRegistrationToken(api.loresSecrets)
	if err != nil {
		api.internalError(writer, request, "generate Lore server registration token", err)
		return
	}
	token, err := api.loreServers.CreateRegistrationToken(request.Context(), actor,
		request.PathValue("organization"), platform.CreateLoreServerRegistrationTokenInput{
			Digest: digest, ExpiresAt: expiresAt,
		})
	if err != nil {
		api.loreServerError(writer, request, "create Lore server registration token", err)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writeJSON(writer, http.StatusCreated, map[string]any{"token": token, "value": raw})
}

func (api *API) registerLoreServer(writer http.ResponseWriter, request *http.Request) {
	if api.loreServers == nil || api.loresSecrets == nil || api.loresTokenKeyID == "" {
		writeProblem(writer, http.StatusServiceUnavailable, "lore_servers_unavailable",
			"Lore server registration is unavailable")
		return
	}
	raw, ok := loreServerBearerToken(writer, request, auth.ValidLoreServerRegistrationToken)
	if !ok {
		return
	}
	var input struct {
		Name              string         `json:"name"`
		PublicURL         string         `json:"publicUrl"`
		LoreBuildVersion  string         `json:"loreBuildVersion"`
		HookModuleVersion string         `json:"hookModuleVersion"`
		HealthMetadata    map[string]any `json:"healthMetadata"`
	}
	if !decodeJSON(writer, request, &input) {
		return
	}
	credential, credentialDigest, err := auth.NewLoreServerCredential(api.loresSecrets)
	if err != nil {
		api.internalError(writer, request, "generate Lore server credential", err)
		return
	}
	credentialExpiresAt := time.Now().UTC().Add(loreServerCredentialAge)
	server, err := api.loreServers.RegisterServer(request.Context(), api.loresSecrets.Digest(raw),
		platform.RegisterLoreServerInput{
			Name: input.Name, PublicURL: input.PublicURL,
			CredentialDigest: credentialDigest, CredentialKeyID: api.loresTokenKeyID,
			CredentialExpiresAt: credentialExpiresAt, LoreBuildVersion: input.LoreBuildVersion,
			HookModuleVersion: input.HookModuleVersion, HealthMetadata: input.HealthMetadata,
			AllowPrivateServers: api.loreAllowPrivateServers,
		})
	if err != nil {
		api.loreServerError(writer, request, "register Lore server", err)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writeJSON(writer, http.StatusCreated, map[string]any{
		"server": server, "credential": credential, "credentialExpiresAt": credentialExpiresAt,
	})
}

func (api *API) heartbeatLoreServer(writer http.ResponseWriter, request *http.Request) {
	if api.loreServers == nil || api.loresSecrets == nil || api.loresTokenKeyID == "" {
		writeProblem(writer, http.StatusServiceUnavailable, "lore_servers_unavailable",
			"Lore server heartbeat is unavailable")
		return
	}
	raw, ok := loreServerBearerToken(writer, request, auth.ValidLoreServerCredential)
	if !ok {
		return
	}
	var input struct {
		LoreBuildVersion  string         `json:"loreBuildVersion"`
		HookModuleVersion string         `json:"hookModuleVersion"`
		HealthMetadata    map[string]any `json:"healthMetadata"`
	}
	if !decodeJSON(writer, request, &input) {
		return
	}
	now := time.Now().UTC()
	server, err := api.loreServers.AuthenticateServer(request.Context(), api.loresSecrets.Digest(raw),
		api.loresTokenKeyID, now)
	if err != nil {
		api.loreServerError(writer, request, "authenticate Lore server", err)
		return
	}
	if err := api.loreServers.UpdateServerHealth(request.Context(), server.ID, now,
		input.LoreBuildVersion, input.HookModuleVersion, input.HealthMetadata); err != nil {
		api.loreServerError(writer, request, "update Lore server heartbeat", err)
		return
	}
	server.LoreBuildVersion = strings.TrimSpace(input.LoreBuildVersion)
	server.LastSeenAt = &now
	server.HealthMetadata = input.HealthMetadata
	if server.HealthMetadata == nil {
		server.HealthMetadata = make(map[string]any)
	}
	server.HealthMetadata["hookModuleVersion"] = strings.TrimSpace(input.HookModuleVersion)
	writeJSON(writer, http.StatusOK, map[string]any{"server": server})
}

func (api *API) listLoreServers(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	if api.loreServers == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "lore_servers_unavailable",
			"Lore server settings are unavailable")
		return
	}
	servers, err := api.loreServers.ListServers(request.Context(), actor, request.PathValue("organization"))
	if err != nil {
		api.loreServerError(writer, request, "list Lore servers", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"servers": servers})
}

func (api *API) revokeLoreServer(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	if api.loreServers == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "lore_servers_unavailable",
			"Lore server settings are unavailable")
		return
	}
	if err := api.loreServers.RevokeServer(request.Context(), actor, request.PathValue("organization"),
		request.PathValue("loreServerID")); err != nil {
		api.loreServerError(writer, request, "revoke Lore server", err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (api *API) getDefaultLoreServer(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	if api.loreServers == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "lore_servers_unavailable",
			"Lore server settings are unavailable")
		return
	}
	server, err := api.loreServers.GetOrganizationDefaultServer(request.Context(), actor,
		request.PathValue("organization"))
	if err != nil {
		api.loreServerError(writer, request, "get default Lore server", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"server": server})
}

func (api *API) setDefaultLoreServer(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	if api.loreServers == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "lore_servers_unavailable",
			"Lore server settings are unavailable")
		return
	}
	var input struct {
		LoreServerID *string `json:"loreServerId"`
	}
	if !decodeJSON(writer, request, &input) {
		return
	}
	serverID := ""
	if input.LoreServerID != nil {
		serverID = strings.TrimSpace(*input.LoreServerID)
	}
	server, err := api.loreServers.SetOrganizationDefaultServer(request.Context(), actor,
		request.PathValue("organization"), serverID)
	if err != nil {
		api.loreServerError(writer, request, "set default Lore server", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"server": server})
}

func loreServerBearerToken(
	writer http.ResponseWriter,
	request *http.Request,
	valid func(string) bool,
) (string, bool) {
	authorization := strings.TrimSpace(request.Header.Get("Authorization"))
	scheme, raw, found := strings.Cut(authorization, " ")
	raw = strings.TrimSpace(raw)
	if !found || !strings.EqualFold(scheme, "Bearer") || !valid(raw) {
		writer.Header().Set("WWW-Authenticate", "Bearer")
		writeProblem(writer, http.StatusUnauthorized, "invalid_credential", "A valid Lore server credential is required")
		return "", false
	}
	return raw, true
}

func (api *API) loreServerError(
	writer http.ResponseWriter,
	request *http.Request,
	operation string,
	err error,
) {
	var selectionError *platform.LoreServerSelectionError
	switch {
	case errors.As(err, &selectionError):
		writeProblem(writer, http.StatusConflict, selectionError.Reason, selectionError.Error())
	case errors.Is(err, auth.ErrInvalidLoreServerRegistrationToken),
		errors.Is(err, auth.ErrInvalidLoreServerCredential):
		writer.Header().Set("WWW-Authenticate", "Bearer")
		writeProblem(writer, http.StatusUnauthorized, "invalid_credential", "The Lore server credential is invalid")
	case errors.Is(err, platform.ErrInvalidInput):
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "Lore server fields are invalid")
	case errors.Is(err, platform.ErrForbidden):
		writeProblem(writer, http.StatusForbidden, "forbidden", "This operation is not permitted")
	case errors.Is(err, platform.ErrNotFound):
		writeProblem(writer, http.StatusNotFound, "not_found", "The Lore server was not found")
	case errors.Is(err, platform.ErrConflict):
		writeProblem(writer, http.StatusConflict, "conflict", "The Lore server is already registered")
	default:
		api.internalError(writer, request, operation, err)
	}
}
