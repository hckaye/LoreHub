package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/lorehub/lorehub/services/api/internal/auth"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

const (
	runnerRegistrationTokenLifetime = time.Hour
	runnerCredentialLifetime        = 365 * 24 * time.Hour
)

type registerRunnerRequest struct {
	Name          string   `json:"name"`
	Labels        []string `json:"labels"`
	Version       string   `json:"version"`
	RunnerVersion string   `json:"runnerVersion"`
}

func (api *API) createOrganizationRunnerRegistrationToken(
	writer http.ResponseWriter,
	request *http.Request,
) {
	actor, scope, ok := api.organizationRunnerScope(writer, request)
	if !ok {
		return
	}
	api.createRunnerRegistrationToken(writer, request, actor, scope)
}

func (api *API) createRepositoryRunnerRegistrationToken(
	writer http.ResponseWriter,
	request *http.Request,
) {
	actor, scope, ok := api.repositoryRunnerScope(writer, request)
	if !ok {
		return
	}
	api.createRunnerRegistrationToken(writer, request, actor, scope)
}

func (api *API) createRunnerRegistrationToken(
	writer http.ResponseWriter,
	request *http.Request,
	actor platform.User,
	scope platform.RunnerScope,
) {
	writer.Header().Set("Cache-Control", "private, no-store")
	raw, digest, err := auth.NewRunnerRegistrationToken(api.runnerSecrets)
	if err != nil {
		api.internalError(writer, request, "generate runner registration token", err)
		return
	}
	token, err := api.runners.CreateRegistrationToken(
		request.Context(), actor, platform.CreateRunnerRegistrationTokenInput{
			Scope: scope, Digest: digest,
			ExpiresAt: time.Now().UTC().Add(runnerRegistrationTokenLifetime),
		},
	)
	if err != nil {
		api.platformError(writer, request, "create runner registration token", err)
		return
	}
	writeJSON(writer, http.StatusCreated, map[string]any{
		"token": raw, "expiresAt": token.ExpiresAt,
	})
}

func (api *API) listOrganizationRunners(writer http.ResponseWriter, request *http.Request) {
	actor, scope, ok := api.organizationRunnerScope(writer, request)
	if !ok {
		return
	}
	api.listRunners(writer, request, actor, scope)
}

func (api *API) listRepositoryRunners(writer http.ResponseWriter, request *http.Request) {
	actor, scope, ok := api.repositoryRunnerScope(writer, request)
	if !ok {
		return
	}
	api.listRunners(writer, request, actor, scope)
}

func (api *API) listRunners(
	writer http.ResponseWriter,
	request *http.Request,
	actor platform.User,
	scope platform.RunnerScope,
) {
	writer.Header().Set("Cache-Control", "private, no-store")
	runners, err := api.runners.ListRunners(request.Context(), actor, scope)
	if err != nil {
		api.platformError(writer, request, "list runners", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"totalCount": len(runners), "runners": runners,
	})
}

func (api *API) revokeOrganizationRunner(writer http.ResponseWriter, request *http.Request) {
	actor, scope, ok := api.organizationRunnerScope(writer, request)
	if !ok {
		return
	}
	api.revokeRunner(writer, request, actor, scope)
}

func (api *API) revokeRepositoryRunner(writer http.ResponseWriter, request *http.Request) {
	actor, scope, ok := api.repositoryRunnerScope(writer, request)
	if !ok {
		return
	}
	api.revokeRunner(writer, request, actor, scope)
}

func (api *API) revokeRunner(
	writer http.ResponseWriter,
	request *http.Request,
	actor platform.User,
	scope platform.RunnerScope,
) {
	writer.Header().Set("Cache-Control", "private, no-store")
	if err := api.runners.RevokeRunner(
		request.Context(), actor, scope, request.PathValue("runnerID"),
	); err != nil {
		api.platformError(writer, request, "revoke runner", err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (api *API) registerRunner(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	if api.runners == nil || api.runnerSecrets == nil || api.runnerCredentialKeyID == "" {
		writeProblem(writer, http.StatusServiceUnavailable, "runners_unavailable",
			"Runner registration is unavailable")
		return
	}
	rawRegistrationToken, ok := runnerRegistrationBearerToken(request.Header.Get("Authorization"))
	if !ok || !auth.ValidRunnerRegistrationToken(rawRegistrationToken) {
		writeRunnerRegistrationUnauthorized(writer)
		return
	}
	var input registerRunnerRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	version := strings.TrimSpace(input.Version)
	if input.RunnerVersion != "" {
		if version != "" {
			writeProblem(writer, http.StatusBadRequest, "invalid_input",
				"Runner fields are invalid")
			return
		}
		version = strings.TrimSpace(input.RunnerVersion)
	}
	if !validRunnerRegistrationRequest(input.Name, input.Labels, version) {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "Runner fields are invalid")
		return
	}
	credential, credentialDigest, err := auth.NewRunnerCredential(api.runnerSecrets)
	if err != nil {
		api.internalError(writer, request, "generate runner credential", err)
		return
	}
	registration, err := api.runners.ConsumeRegistrationToken(
		request.Context(), api.runnerSecrets.Digest(rawRegistrationToken),
	)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidRunnerToken) {
			writeRunnerRegistrationUnauthorized(writer)
			return
		}
		api.internalError(writer, request, "consume runner registration token", err)
		return
	}
	runner, err := api.runners.RegisterRunner(request.Context(), platform.RegisterRunnerInput{
		RegistrationTokenID: registration.ID,
		Name:                input.Name,
		Labels:              input.Labels,
		CredentialDigest:    credentialDigest,
		CredentialKeyID:     api.runnerCredentialKeyID,
		CredentialExpiresAt: time.Now().UTC().Add(runnerCredentialLifetime),
		RunnerVersion:       version,
	})
	if err != nil {
		if errors.Is(err, auth.ErrInvalidRunnerToken) {
			writeRunnerRegistrationUnauthorized(writer)
			return
		}
		api.platformError(writer, request, "register runner", err)
		return
	}
	writeJSON(writer, http.StatusCreated, map[string]any{
		"runner": runner, "token": credential,
	})
}

func (api *API) organizationRunnerScope(
	writer http.ResponseWriter,
	request *http.Request,
) (platform.User, platform.RunnerScope, bool) {
	if api.runners == nil || api.runnerSecrets == nil || api.identityStore == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "runners_unavailable",
			"Runner management is unavailable")
		return platform.User{}, platform.RunnerScope{}, false
	}
	actor, ok := api.actor(writer, request)
	if !ok {
		return platform.User{}, platform.RunnerScope{}, false
	}
	organization, err := api.identityStore.Organization(
		request.Context(), &actor, request.PathValue("organization"),
	)
	if err != nil {
		api.platformError(writer, request, "find runner organization", err)
		return platform.User{}, platform.RunnerScope{}, false
	}
	return actor, platform.RunnerScope{OrganizationID: organization.ID}, true
}

func (api *API) repositoryRunnerScope(
	writer http.ResponseWriter,
	request *http.Request,
) (platform.User, platform.RunnerScope, bool) {
	if api.runners == nil || api.runnerSecrets == nil || api.actions == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "runners_unavailable",
			"Runner management is unavailable")
		return platform.User{}, platform.RunnerScope{}, false
	}
	actor, ok := api.actor(writer, request)
	if !ok {
		return platform.User{}, platform.RunnerScope{}, false
	}
	access, err := api.actions.RepositoryForActions(
		request.Context(), request.PathValue("owner"), request.PathValue("repository"), actor.ID,
	)
	if err != nil {
		api.actionsError(writer, request, "find runner repository", err)
		return platform.User{}, platform.RunnerScope{}, false
	}
	return actor, platform.RunnerScope{
		OrganizationID: access.OrganizationID,
		RepositoryID:   access.ID,
	}, true
}

func runnerRegistrationBearerToken(authorization string) (string, bool) {
	parts := strings.Fields(authorization)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	return parts[1], true
}

func writeRunnerRegistrationUnauthorized(writer http.ResponseWriter) {
	writer.Header().Set("WWW-Authenticate", `Bearer realm="runner-registration"`)
	writeProblem(writer, http.StatusUnauthorized, "runner_registration_token_required",
		"A valid runner registration token is required")
}

func validRunnerRegistrationRequest(name string, labels []string, version string) bool {
	name = strings.TrimSpace(name)
	if !validRunnerRequestText(name, 100) || !validRunnerRequestText(version, 64) ||
		len(labels) == 0 || len(labels) > 100 {
		return false
	}
	for _, label := range labels {
		if !validRunnerRequestText(strings.TrimSpace(label), 100) {
			return false
		}
	}
	return true
}

func validRunnerRequestText(value string, limit int) bool {
	if value == "" || utf8.RuneCountInString(value) > limit {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
