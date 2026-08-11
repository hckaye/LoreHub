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

const personalAccessTokenMaxAge = 366 * 24 * time.Hour

func (api *API) listPersonalAccessTokens(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.personalAccessTokenActor(writer, request)
	if !ok {
		return
	}
	if api.personalAccessTokens == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "personal_access_tokens_unavailable",
			"Personal access tokens are unavailable")
		return
	}
	tokens, err := api.personalAccessTokens.ListPersonalAccessTokens(request.Context(), actor)
	if err != nil {
		api.personalAccessTokenError(writer, request, "list personal access tokens", err)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writeJSON(writer, http.StatusOK, map[string]any{"tokens": tokens})
}

func (api *API) createPersonalAccessToken(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.personalAccessTokenActor(writer, request)
	if !ok {
		return
	}
	if api.personalAccessTokens == nil || api.secrets == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "personal_access_tokens_unavailable",
			"Personal access tokens are unavailable")
		return
	}
	var input struct {
		Name      string    `json:"name"`
		Scopes    []string  `json:"scopes"`
		ExpiresAt time.Time `json:"expiresAt"`
	}
	if !decodeJSON(writer, request, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if !validPersonalAccessTokenRequest(input.Name, input.Scopes, input.ExpiresAt, time.Now().UTC()) {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "Personal access token fields are invalid")
		return
	}
	raw, digest, err := auth.NewPersonalAccessToken(api.secrets)
	if err != nil {
		api.internalError(writer, request, "generate personal access token", err)
		return
	}
	token, err := api.personalAccessTokens.CreatePersonalAccessToken(
		request.Context(),
		actor,
		platform.CreatePersonalAccessTokenInput{
			Name:      input.Name,
			Prefix:    auth.PersonalAccessTokenPrefix(raw),
			Digest:    digest,
			Scopes:    input.Scopes,
			ExpiresAt: input.ExpiresAt,
		},
	)
	if err != nil {
		api.personalAccessTokenError(writer, request, "create personal access token", err)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writeJSON(writer, http.StatusCreated, map[string]any{"token": token, "value": raw})
}

func (api *API) revokePersonalAccessToken(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.personalAccessTokenActor(writer, request)
	if !ok {
		return
	}
	if api.personalAccessTokens == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "personal_access_tokens_unavailable",
			"Personal access tokens are unavailable")
		return
	}
	if err := api.personalAccessTokens.RevokePersonalAccessToken(
		request.Context(),
		actor,
		request.PathValue("tokenID"),
	); err != nil {
		api.personalAccessTokenError(writer, request, "revoke personal access token", err)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusNoContent)
}

func (api *API) personalAccessTokenActor(
	writer http.ResponseWriter,
	request *http.Request,
) (platform.User, bool) {
	authorization := strings.TrimSpace(request.Header.Get("Authorization"))
	scheme, raw, found := strings.Cut(authorization, " ")
	if found && strings.EqualFold(scheme, "Bearer") && strings.HasPrefix(strings.TrimSpace(raw), "lhp_") {
		writeProblem(writer, http.StatusForbidden, "personal_access_token_forbidden",
			"A personal access token cannot manage personal access tokens")
		return platform.User{}, false
	}
	return api.actor(writer, request)
}

func (api *API) personalAccessTokenError(
	writer http.ResponseWriter,
	request *http.Request,
	operation string,
	err error,
) {
	switch {
	case errors.Is(err, platform.ErrInvalidInput):
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "Personal access token fields are invalid")
	case errors.Is(err, platform.ErrForbidden):
		writeProblem(writer, http.StatusForbidden, "forbidden", "This operation is not permitted")
	case errors.Is(err, platform.ErrNotFound):
		writeProblem(writer, http.StatusNotFound, "not_found", "Personal access token was not found")
	default:
		api.internalError(writer, request, operation, err)
	}
}

func validPersonalAccessTokenRequest(name string, scopes []string, expiresAt time.Time, now time.Time) bool {
	if name == "" || utf8.RuneCountInString(name) > 80 || !auth.ValidPersonalAccessTokenScopes(scopes) ||
		expiresAt.IsZero() || !expiresAt.After(now.Add(time.Minute)) ||
		expiresAt.After(now.Add(personalAccessTokenMaxAge)) {
		return false
	}
	for _, character := range name {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
