package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type providerResponse struct {
	ID string `json:"id"`
}

func (api *API) providers(writer http.ResponseWriter, _ *http.Request) {
	if !api.interactiveAuthenticationAvailable() {
		writeProblem(writer, http.StatusServiceUnavailable, "authentication_unavailable",
			"Interactive authentication is not configured")
		return
	}
	providers := []providerResponse{{ID: "password"}}
	for _, provider := range api.loginProviders {
		providers = append(providers, providerResponse{ID: provider})
	}
	writeJSON(writer, http.StatusOK, map[string]any{"providers": providers})
}

func (api *API) dashboard(writer http.ResponseWriter, request *http.Request) {
	if api.identityStore == nil {
		api.identityUnavailable(writer)
		return
	}
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	dashboard, err := api.identityStore.Dashboard(request.Context(), actor)
	if err != nil {
		api.internalError(writer, request, "get dashboard", err)
		return
	}
	writeJSON(writer, http.StatusOK, dashboard)
}

func (api *API) search(writer http.ResponseWriter, request *http.Request) {
	if api.identityStore == nil {
		api.identityUnavailable(writer)
		return
	}
	viewer, ok := api.ResolveOptionalActor(writer, request)
	if !ok {
		return
	}
	query := strings.TrimSpace(request.URL.Query().Get("q"))
	if len([]rune(query)) > 160 {
		writeProblem(writer, http.StatusBadRequest, "invalid_query", "The search query is too long")
		return
	}
	kind := request.URL.Query().Get("type")
	if kind == "" {
		kind = "all"
	}
	if kind != "all" && kind != "repositories" && kind != "organizations" && kind != "users" {
		writeProblem(writer, http.StatusBadRequest, "invalid_type", "The search type is invalid")
		return
	}
	limit, err := queryLimit(request.URL.Query().Get("limit"), 20, 50)
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_limit", "The search limit is invalid")
		return
	}
	results, err := api.identityStore.Search(request.Context(), viewer, query, kind, limit)
	if err != nil {
		api.internalError(writer, request, "search LoreHub", err)
		return
	}
	writeJSON(writer, http.StatusOK, results)
}

func (api *API) userProfile(writer http.ResponseWriter, request *http.Request) {
	if api.identityStore == nil {
		api.identityUnavailable(writer)
		return
	}
	viewer, ok := api.ResolveOptionalActor(writer, request)
	if !ok {
		return
	}
	profile, err := api.identityStore.UserProfile(request.Context(), viewer, request.PathValue("username"))
	if err != nil {
		api.platformError(writer, request, "get user profile", err)
		return
	}
	writeJSON(writer, http.StatusOK, profile)
}

func (api *API) userRepositories(writer http.ResponseWriter, request *http.Request) {
	if api.identityStore == nil {
		api.identityUnavailable(writer)
		return
	}
	viewer, ok := api.ResolveOptionalActor(writer, request)
	if !ok {
		return
	}
	repositories, err := api.identityStore.UserRepositories(
		request.Context(), viewer, request.PathValue("username"),
	)
	if err != nil {
		api.platformError(writer, request, "list user repositories", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"repositories": repositories})
}

func (api *API) updateProfile(writer http.ResponseWriter, request *http.Request) {
	if api.identityStore == nil {
		api.identityUnavailable(writer)
		return
	}
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	var input struct {
		DisplayName *string `json:"displayName"`
		Bio         *string `json:"bio"`
		AvatarURL   *string `json:"avatarUrl"`
		WebsiteURL  *string `json:"websiteUrl"`
		Location    *string `json:"location"`
		Company     *string `json:"company"`
		Pronouns    *string `json:"pronouns"`
	}
	if !decodeJSON(writer, request, &input) {
		return
	}
	if !validProfileFields(input.DisplayName, input.Bio, input.AvatarURL, input.WebsiteURL, input.Location,
		input.Company, input.Pronouns) || !validOptionalURL(input.AvatarURL) || !validOptionalURL(input.WebsiteURL) {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "Profile fields are invalid")
		return
	}
	profile, err := api.identityStore.UpdateProfile(request.Context(), actor, platform.UpdateProfileInput{
		DisplayName: input.DisplayName,
		Bio:         input.Bio,
		AvatarURL:   input.AvatarURL,
		WebsiteURL:  input.WebsiteURL,
		Location:    input.Location,
		Company:     input.Company,
		Pronouns:    input.Pronouns,
	})
	if err != nil {
		api.platformError(writer, request, "update profile", err)
		return
	}
	writeJSON(writer, http.StatusOK, profile)
}

func (api *API) notifications(writer http.ResponseWriter, request *http.Request) {
	if api.identityStore == nil {
		api.identityUnavailable(writer)
		return
	}
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	unreadOnly, err := optionalBool(request.URL.Query().Get("unread"))
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_unread", "The unread filter is invalid")
		return
	}
	limit, err := queryLimit(request.URL.Query().Get("limit"), 30, 100)
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_limit", "The notification limit is invalid")
		return
	}
	page, err := api.identityStore.ListNotifications(request.Context(), actor, unreadOnly, limit)
	if err != nil {
		api.internalError(writer, request, "list notifications", err)
		return
	}
	writeJSON(writer, http.StatusOK, page)
}

func (api *API) unreadNotificationCount(writer http.ResponseWriter, request *http.Request) {
	if api.identityStore == nil {
		api.identityUnavailable(writer)
		return
	}
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	count, err := api.identityStore.UnreadNotificationCount(request.Context(), actor)
	if err != nil {
		api.internalError(writer, request, "count notifications", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]int64{"count": count})
}

func (api *API) markNotificationRead(writer http.ResponseWriter, request *http.Request) {
	if api.identityStore == nil {
		api.identityUnavailable(writer)
		return
	}
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	if _, err := uuid.Parse(request.PathValue("notificationID")); err != nil {
		writeProblem(writer, http.StatusNotFound, "not_found", "The notification was not found")
		return
	}
	if err := api.identityStore.MarkNotificationRead(
		request.Context(), actor, request.PathValue("notificationID"),
	); err != nil {
		api.platformError(writer, request, "mark notification read", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]bool{"read": true})
}

func (api *API) markAllNotificationsRead(writer http.ResponseWriter, request *http.Request) {
	if api.identityStore == nil {
		api.identityUnavailable(writer)
		return
	}
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	if err := api.identityStore.MarkAllNotificationsRead(request.Context(), actor); err != nil {
		api.internalError(writer, request, "mark all notifications read", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]bool{"read": true})
}

func (api *API) notificationPreferences(writer http.ResponseWriter, request *http.Request) {
	if api.identityStore == nil {
		api.identityUnavailable(writer)
		return
	}
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	preferences, err := api.identityStore.NotificationPreferences(request.Context(), actor)
	if err != nil {
		api.internalError(writer, request, "get notification preferences", err)
		return
	}
	writeJSON(writer, http.StatusOK, preferences)
}

func (api *API) updateNotificationPreferences(writer http.ResponseWriter, request *http.Request) {
	if api.identityStore == nil {
		api.identityUnavailable(writer)
		return
	}
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	var input struct {
		InAppEnabled      *bool `json:"inAppEnabled"`
		EmailEnabled      *bool `json:"emailEnabled"`
		MentionEnabled    *bool `json:"mentionEnabled"`
		TeamEnabled       *bool `json:"teamEnabled"`
		RepositoryEnabled *bool `json:"repositoryEnabled"`
	}
	if !decodeJSON(writer, request, &input) {
		return
	}
	preferences, err := api.identityStore.UpdateNotificationPreferences(request.Context(), actor,
		platform.UpdateNotificationPreferencesInput{
			InAppEnabled: input.InAppEnabled, EmailEnabled: input.EmailEnabled,
			MentionEnabled: input.MentionEnabled, TeamEnabled: input.TeamEnabled,
			RepositoryEnabled: input.RepositoryEnabled,
		})
	if err != nil {
		api.internalError(writer, request, "update notification preferences", err)
		return
	}
	writeJSON(writer, http.StatusOK, preferences)
}

func (api *API) identityUnavailable(writer http.ResponseWriter) {
	writeProblem(writer, http.StatusServiceUnavailable, "identity_unavailable", "Identity services are unavailable")
}

func validProfileFields(values ...*string) bool {
	limits := []int{160, 2_000, 500, 500, 160, 160, 80}
	for index, value := range values {
		if value != nil && len([]rune(*value)) > limits[index] {
			return false
		}
	}
	return true
}

func validOptionalURL(value *string) bool {
	if value == nil || strings.TrimSpace(*value) == "" {
		return true
	}
	parsed, err := url.Parse(strings.TrimSpace(*value))
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func queryLimit(value string, fallback int, maximum int) (int, error) {
	if value == "" {
		return fallback, nil
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 || limit > maximum {
		return 0, errors.New("invalid limit")
	}
	return limit, nil
}

func optionalBool(value string) (bool, error) {
	if value == "" {
		return false, nil
	}
	parsed, err := strconv.ParseBool(value)
	return parsed, err
}
