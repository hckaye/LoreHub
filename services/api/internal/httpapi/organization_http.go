package httpapi

import (
	"net/http"
	"strings"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func (api *API) organization(writer http.ResponseWriter, request *http.Request) {
	if api.identityStore == nil {
		api.identityUnavailable(writer)
		return
	}
	viewer, ok := api.ResolveOptionalActor(writer, request)
	if !ok {
		return
	}
	organization, err := api.identityStore.Organization(request.Context(), viewer, request.PathValue("organization"))
	if err != nil {
		api.platformError(writer, request, "get organization", err)
		return
	}
	writeJSON(writer, http.StatusOK, organization)
}

func (api *API) organizationRepositories(writer http.ResponseWriter, request *http.Request) {
	if api.identityStore == nil {
		api.identityUnavailable(writer)
		return
	}
	viewer, ok := api.ResolveOptionalActor(writer, request)
	if !ok {
		return
	}
	repositories, err := api.identityStore.OrganizationRepositories(
		request.Context(), viewer, request.PathValue("organization"),
	)
	if err != nil {
		api.platformError(writer, request, "list organization repositories", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"repositories": repositories})
}

func (api *API) updateOrganization(writer http.ResponseWriter, request *http.Request) {
	if api.identityStore == nil {
		api.identityUnavailable(writer)
		return
	}
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	var input struct {
		DisplayName                 *string `json:"displayName"`
		Description                 *string `json:"description"`
		Visibility                  *string `json:"visibility"`
		WebsiteURL                  *string `json:"websiteUrl"`
		ContactEmail                *string `json:"contactEmail"`
		DefaultRepositoryVisibility *string `json:"defaultRepositoryVisibility"`
	}
	if !decodeJSON(writer, request, &input) {
		return
	}
	if !validOptionalText(input.DisplayName, 160) || !validOptionalText(input.Description, 10_000) ||
		!validOptionalText(input.WebsiteURL, 500) || !validOptionalURL(input.WebsiteURL) ||
		!validOptionalText(input.ContactEmail, 320) ||
		!validVisibilityPointer(input.Visibility) || !validVisibilityPointer(input.DefaultRepositoryVisibility) {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "Organization fields are invalid")
		return
	}
	organization, err := api.identityStore.UpdateOrganization(request.Context(), actor,
		request.PathValue("organization"), platform.UpdateOrganizationInput{
			DisplayName: input.DisplayName, Description: input.Description, Visibility: input.Visibility,
			WebsiteURL: input.WebsiteURL, ContactEmail: input.ContactEmail,
			DefaultRepositoryVisibility: input.DefaultRepositoryVisibility,
		})
	if err != nil {
		api.platformError(writer, request, "update organization", err)
		return
	}
	writeJSON(writer, http.StatusOK, organization)
}

func (api *API) teams(writer http.ResponseWriter, request *http.Request) {
	if api.identityStore == nil {
		api.identityUnavailable(writer)
		return
	}
	viewer, ok := api.ResolveOptionalActor(writer, request)
	if !ok {
		return
	}
	teams, err := api.identityStore.Teams(request.Context(), viewer, request.PathValue("organization"))
	if err != nil {
		api.platformError(writer, request, "list teams", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"teams": teams})
}

func (api *API) createIdentityTeam(writer http.ResponseWriter, request *http.Request) {
	if api.identityStore == nil {
		api.identityUnavailable(writer)
		return
	}
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	var input struct {
		Slug        string `json:"slug"`
		DisplayName string `json:"displayName"`
		Description string `json:"description"`
	}
	if !decodeJSON(writer, request, &input) {
		return
	}
	input.Slug = strings.TrimSpace(strings.ToLower(input.Slug))
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if input.DisplayName == "" || len([]rune(input.DisplayName)) > 160 || len([]rune(input.Description)) > 10_000 {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "Team fields are invalid")
		return
	}
	team, err := api.identityStore.CreateTeam(request.Context(), actor, request.PathValue("organization"),
		platform.SetTeamInput{Slug: input.Slug, DisplayName: input.DisplayName, Description: input.Description})
	if err != nil {
		api.platformError(writer, request, "create team", err)
		return
	}
	writeJSON(writer, http.StatusCreated, team)
}

func (api *API) team(writer http.ResponseWriter, request *http.Request) {
	if api.identityStore == nil {
		api.identityUnavailable(writer)
		return
	}
	viewer, ok := api.ResolveOptionalActor(writer, request)
	if !ok {
		return
	}
	team, err := api.identityStore.Team(request.Context(), viewer, request.PathValue("organization"),
		request.PathValue("team"))
	if err != nil {
		api.platformError(writer, request, "get team", err)
		return
	}
	response := map[string]any{"team": team}
	if team.ViewerRole != "" {
		members, err := api.identityStore.TeamMembers(request.Context(), viewer, request.PathValue("organization"),
			request.PathValue("team"))
		if err != nil {
			api.platformError(writer, request, "list team members", err)
			return
		}
		response["members"] = members
	}
	writeJSON(writer, http.StatusOK, response)
}

func (api *API) updateIdentityTeam(writer http.ResponseWriter, request *http.Request) {
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
		Description *string `json:"description"`
	}
	if !decodeJSON(writer, request, &input) {
		return
	}
	if !validOptionalText(input.DisplayName, 160) || !validOptionalText(input.Description, 10_000) {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "Team fields are invalid")
		return
	}
	team, err := api.identityStore.UpdateTeam(request.Context(), actor, request.PathValue("organization"),
		request.PathValue("team"), platform.SetTeamInput{
			DisplayName: valueOrEmpty(input.DisplayName),
			Description: valueOrEmpty(input.Description),
		})
	if err != nil {
		api.platformError(writer, request, "update team", err)
		return
	}
	writeJSON(writer, http.StatusOK, team)
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (api *API) teamMembers(writer http.ResponseWriter, request *http.Request) {
	if api.identityStore == nil {
		api.identityUnavailable(writer)
		return
	}
	viewer, ok := api.ResolveOptionalActor(writer, request)
	if !ok {
		return
	}
	members, err := api.identityStore.TeamMembers(request.Context(), viewer, request.PathValue("organization"),
		request.PathValue("team"))
	if err != nil {
		api.platformError(writer, request, "list team members", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"members": members})
}

func (api *API) addTeamMember(writer http.ResponseWriter, request *http.Request) {
	if api.identityStore == nil {
		api.identityUnavailable(writer)
		return
	}
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	var input struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if !decodeJSON(writer, request, &input) {
		return
	}
	input.Username = strings.TrimSpace(input.Username)
	if input.Username == "" || (input.Role != "member" && input.Role != "maintainer") {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "Team membership fields are invalid")
		return
	}
	member, err := api.identityStore.AddTeamMember(request.Context(), actor, request.PathValue("organization"),
		request.PathValue("team"), input.Username, input.Role)
	if err != nil {
		api.platformError(writer, request, "add team member", err)
		return
	}
	writeJSON(writer, http.StatusCreated, member)
}

func (api *API) removeTeamMember(writer http.ResponseWriter, request *http.Request) {
	if api.identityStore == nil {
		api.identityUnavailable(writer)
		return
	}
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	err := api.identityStore.RemoveTeamMember(request.Context(), actor, request.PathValue("organization"),
		request.PathValue("team"), request.PathValue("username"))
	if err != nil {
		api.platformError(writer, request, "remove team member", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]bool{"removed": true})
}

func (api *API) updateRepositorySettings(writer http.ResponseWriter, request *http.Request) {
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
		Description *string `json:"description"`
		Visibility  *string `json:"visibility"`
		HomepageURL *string `json:"homepageUrl"`
	}
	if !decodeJSON(writer, request, &input) {
		return
	}
	if !validOptionalText(input.DisplayName, 200) || !validOptionalText(input.Description, 10_000) ||
		!validOptionalText(input.HomepageURL, 500) || !validOptionalURL(input.HomepageURL) ||
		!validVisibilityPointer(input.Visibility) {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "Repository settings are invalid")
		return
	}
	repository, err := api.identityStore.UpdateRepositorySettings(request.Context(), actor,
		request.PathValue("owner"), request.PathValue("repository"), platform.UpdateRepositorySettingsInput{
			DisplayName: input.DisplayName, Description: input.Description,
			Visibility: input.Visibility, HomepageURL: input.HomepageURL,
		})
	if err != nil {
		api.platformError(writer, request, "update repository settings", err)
		return
	}
	writeJSON(writer, http.StatusOK, repository)
}

func (api *API) repositorySettings(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	repository, err := api.store.RepositoryForWrite(request.Context(), actor, request.PathValue("owner"),
		request.PathValue("repository"))
	if err != nil {
		api.platformError(writer, request, "get repository settings", err)
		return
	}
	writeJSON(writer, http.StatusOK, repository)
}

func validOptionalText(value *string, limit int) bool {
	return value == nil || len([]rune(*value)) <= limit
}

func validVisibilityPointer(value *string) bool {
	return value == nil || validVisibility(*value)
}
