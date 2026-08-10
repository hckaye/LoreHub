package httpapi

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/url"
	"strings"

	"github.com/lorehub/lorehub/services/api/internal/authz"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func registerAuthorizationRoutes(mux *http.ServeMux, api *API) {
	mux.HandleFunc("GET /api/v1/organizations/{organization}/members", api.listOrganizationMembers)
	mux.HandleFunc("PUT /api/v1/organizations/{organization}/members/{username}", api.setOrganizationMember)
	mux.HandleFunc("DELETE /api/v1/organizations/{organization}/members/{username}", api.removeOrganizationMember)
	mux.HandleFunc("GET /api/v1/organizations/{organization}/teams", api.listTeams)
	mux.HandleFunc("POST /api/v1/organizations/{organization}/teams", api.createTeam)
	mux.HandleFunc("PATCH /api/v1/organizations/{organization}/teams/{team}", api.updateTeam)
	mux.HandleFunc("DELETE /api/v1/organizations/{organization}/teams/{team}", api.deleteTeam)
	mux.HandleFunc("GET /api/v1/organizations/{organization}/teams/{team}/members", api.listTeamMembers)
	mux.HandleFunc("PUT /api/v1/organizations/{organization}/teams/{team}/members/{username}", api.setTeamMember)
	mux.HandleFunc(
		"PUT /api/v1/organizations/{organization}/teams/{team}/repositories/{owner}/{repository}",
		api.setTeamRepositoryRole,
	)
	mux.HandleFunc(
		"DELETE /api/v1/organizations/{organization}/teams/{team}/repositories/{owner}/{repository}",
		api.deleteTeamRepositoryRole,
	)
	mux.HandleFunc("GET /api/v1/repositories/{owner}/{repository}/collaborators", api.listCollaborators)
	mux.HandleFunc("PUT /api/v1/repositories/{owner}/{repository}/collaborators/{username}", api.setCollaborator)
	mux.HandleFunc("DELETE /api/v1/repositories/{owner}/{repository}/collaborators/{username}", api.removeCollaborator)
	mux.HandleFunc("GET /api/v1/repositories/{owner}/{repository}/policy", api.getRepositoryPolicy)
	mux.HandleFunc("PUT /api/v1/repositories/{owner}/{repository}/policy", api.setRepositoryPolicy)
	mux.HandleFunc("PUT /api/v1/repositories/{owner}/{repository}/obliterate/{username}", api.setObliterateGrant)
	mux.HandleFunc(
		"PUT /api/v1/repositories/{owner}/{repository}/service-principals/{principal}",
		api.setServicePrincipalGrant,
	)
	mux.HandleFunc("GET /api/v1/repositories/{owner}/{repository}/links", api.listRepositoryLinks)
	mux.HandleFunc("POST /api/v1/repositories/{owner}/{repository}/links", api.declareRepositoryLink)
}

func (api *API) listOrganizationMembers(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	members, err := api.authorization.ListOrganizationMembers(
		request.Context(), actor, request.PathValue("organization"),
	)
	if err != nil {
		api.platformError(writer, request, "list organization members", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"members": members})
}

func (api *API) setOrganizationMember(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	var input struct {
		Role   string `json:"role"`
		Active bool   `json:"active"`
	}
	if !decodeJSON(writer, request, &input) || input.Role == "" {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "Organization member fields are invalid")
		return
	}
	member, err := api.authorization.SetOrganizationMember(request.Context(), actor,
		request.PathValue("organization"), platform.SetOrganizationMemberInput{
			Username: request.PathValue("username"), Role: input.Role, Active: input.Active,
		})
	if err != nil {
		api.platformError(writer, request, "set organization member", err)
		return
	}
	writeJSON(writer, http.StatusOK, member)
}

func (api *API) removeOrganizationMember(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	member, err := api.authorization.SetOrganizationMember(request.Context(), actor,
		request.PathValue("organization"), platform.SetOrganizationMemberInput{
			Username: request.PathValue("username"), Role: "member", Active: false,
		})
	if err != nil {
		api.platformError(writer, request, "remove organization member", err)
		return
	}
	writeJSON(writer, http.StatusOK, member)
}

func (api *API) jwks(writer http.ResponseWriter, request *http.Request) {
	if api.loreAuth == nil {
		writeProblem(
			writer, http.StatusServiceUnavailable, "authentication_unavailable",
			"Lore authentication is unavailable",
		)
		return
	}
	writer.Header().Set("Cache-Control", "public, max-age=60, must-revalidate")
	writeJSON(writer, http.StatusOK, api.loreAuth.JWKS())
}

func (api *API) listTeams(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	teams, err := api.authorization.ListTeams(request.Context(), actor, request.PathValue("organization"))
	if err != nil {
		api.platformError(writer, request, "list teams", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"teams": teams})
}

func (api *API) createTeam(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	var input platform.SetTeamInput
	if !decodeJSON(writer, request, &input) || input.Slug == "" || input.DisplayName == "" {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "Team fields are invalid")
		return
	}
	team, err := api.authorization.CreateTeam(request.Context(), actor, request.PathValue("organization"), input)
	if err != nil {
		api.platformError(writer, request, "create team", err)
		return
	}
	writeJSON(writer, http.StatusCreated, team)
}

func (api *API) updateTeam(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	var input platform.SetTeamInput
	if !decodeJSON(writer, request, &input) || input.DisplayName == "" {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "Team fields are invalid")
		return
	}
	team, err := api.authorization.UpdateTeam(request.Context(), actor, request.PathValue("organization"),
		request.PathValue("team"), input)
	if err != nil {
		api.platformError(writer, request, "update team", err)
		return
	}
	writeJSON(writer, http.StatusOK, team)
}

func (api *API) deleteTeam(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	err := api.authorization.DeleteTeam(
		request.Context(), actor, request.PathValue("organization"), request.PathValue("team"),
	)
	if err != nil {
		api.platformError(writer, request, "delete team", err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (api *API) listTeamMembers(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	members, err := api.authorization.ListTeamMembers(
		request.Context(), actor, request.PathValue("organization"), request.PathValue("team"),
	)
	if err != nil {
		api.platformError(writer, request, "list team members", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"members": members})
}

func (api *API) setTeamMember(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	var input struct {
		Role   string `json:"role"`
		Active bool   `json:"active"`
	}
	if !decodeJSON(writer, request, &input) || input.Role == "" {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "Team member fields are invalid")
		return
	}
	member, err := api.authorization.SetTeamMember(request.Context(), actor, request.PathValue("organization"),
		request.PathValue("team"), platform.SetTeamMemberInput{
			Username: request.PathValue("username"), Role: input.Role, Active: input.Active,
		})
	if err != nil {
		api.platformError(writer, request, "set team member", err)
		return
	}
	writeJSON(writer, http.StatusOK, member)
}

func (api *API) setTeamRepositoryRole(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	var input platform.SetTeamRepositoryRoleInput
	if !decodeJSON(writer, request, &input) || input.Role == "" {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "Team repository role is invalid")
		return
	}
	result, err := api.authorization.SetTeamRepositoryRole(request.Context(), actor, request.PathValue("organization"),
		request.PathValue("team"), request.PathValue("owner"), request.PathValue("repository"), input)
	if err != nil {
		api.platformError(writer, request, "set team repository role", err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (api *API) deleteTeamRepositoryRole(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	err := api.authorization.DeleteTeamRepositoryRole(request.Context(), actor, request.PathValue("organization"),
		request.PathValue("team"), request.PathValue("owner"), request.PathValue("repository"))
	if err != nil {
		api.platformError(writer, request, "delete team repository role", err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (api *API) listCollaborators(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	users, err := api.authorization.ListRepositoryCollaborators(
		request.Context(), actor, request.PathValue("owner"), request.PathValue("repository"),
	)
	if err != nil {
		api.platformError(writer, request, "list collaborators", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"collaborators": users})
}

func (api *API) setCollaborator(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	var input struct {
		Role   string `json:"role"`
		Active bool   `json:"active"`
	}
	if !decodeJSON(writer, request, &input) || input.Role == "" {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "Collaborator fields are invalid")
		return
	}
	collaborator, err := api.authorization.SetRepositoryCollaborator(request.Context(), actor, request.PathValue("owner"),
		request.PathValue("repository"), platform.SetCollaboratorInput{
			Username: request.PathValue("username"), Role: input.Role, Active: input.Active,
		})
	if err != nil {
		api.platformError(writer, request, "set collaborator", err)
		return
	}
	writeJSON(writer, http.StatusOK, collaborator)
}

func (api *API) removeCollaborator(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	collaborator, err := api.authorization.SetRepositoryCollaborator(request.Context(), actor, request.PathValue("owner"),
		request.PathValue("repository"), platform.SetCollaboratorInput{
			Username: request.PathValue("username"), Role: "read", Active: false,
		})
	if err != nil {
		api.platformError(writer, request, "remove collaborator", err)
		return
	}
	writeJSON(writer, http.StatusOK, collaborator)
}

func (api *API) setRepositoryPolicy(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	var input platform.SetRepositoryPolicyInput
	if !decodeJSON(writer, request, &input) {
		return
	}
	if err := api.authorization.SetRepositoryPolicy(
		request.Context(), actor, request.PathValue("owner"), request.PathValue("repository"), input,
	); err != nil {
		api.platformError(writer, request, "set repository policy", err)
		return
	}
	writeJSON(writer, http.StatusOK, input)
}

func (api *API) getRepositoryPolicy(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	policy, err := api.authorization.GetRepositoryPolicy(request.Context(), actor, request.PathValue("owner"),
		request.PathValue("repository"))
	if err != nil {
		api.platformError(writer, request, "get repository policy", err)
		return
	}
	writeJSON(writer, http.StatusOK, policy)
}

func (api *API) setObliterateGrant(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	var input struct {
		Active bool `json:"active"`
	}
	if !decodeJSON(writer, request, &input) {
		return
	}
	err := api.authorization.SetObliterateGrant(
		request.Context(), actor, request.PathValue("owner"), request.PathValue("repository"),
		request.PathValue("username"), input.Active,
	)
	if err != nil {
		api.platformError(writer, request, "set obliterate grant", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]bool{"active": input.Active})
}

func (api *API) setServicePrincipalGrant(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	var input struct {
		Permissions []string `json:"permissions"`
		Active      bool     `json:"active"`
	}
	if !decodeJSON(writer, request, &input) || len(input.Permissions) == 0 {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "Service principal permissions are required")
		return
	}
	if err := api.authorization.SetServicePrincipalGrant(request.Context(), actor,
		request.PathValue("principal"), request.PathValue("owner"), request.PathValue("repository"),
		input.Permissions, input.Active); err != nil {
		api.platformError(writer, request, "set service principal grant", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"principal": request.PathValue("principal"), "permissions": input.Permissions, "active": input.Active,
	})
}

func (api *API) listRepositoryLinks(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	links, err := api.authorization.ListRepositoryLinks(
		request.Context(), actor, request.PathValue("owner"), request.PathValue("repository"),
	)
	if err != nil {
		api.platformError(writer, request, "list repository links", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"links": links, "support": "declared_only"})
}

func (api *API) declareRepositoryLink(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	var input struct {
		TargetOwner      string `json:"targetOwner"`
		TargetRepository string `json:"targetRepository"`
	}
	if !decodeJSON(writer, request, &input) || input.TargetOwner == "" || input.TargetRepository == "" {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "The target Lore repository is required")
		return
	}
	link, err := api.authorization.DeclareRepositoryLink(
		request.Context(), actor, request.PathValue("owner"), request.PathValue("repository"),
		input.TargetOwner, input.TargetRepository,
	)
	if err != nil {
		api.platformError(writer, request, "declare repository link", err)
		return
	}
	writeJSON(writer, http.StatusCreated, map[string]any{"link": link, "support": "declared_only"})
}

type policyRequest struct {
	UserID           string `json:"userId"`
	ResourceID       string `json:"resourceId"`
	Operation        string `json:"operation"`
	BranchID         string `json:"branchId"`
	BranchName       string `json:"branchName"`
	ProposedRevision string `json:"proposedRevision"`
	ClientIP         string `json:"clientIp"`
}

type loreObservationStore interface {
	ObserveLoreBranchRevision(ctx context.Context, loreRepositoryID, branchID, revision string) error
	DeleteLoreBranchState(ctx context.Context, loreRepositoryID, branchID string) error
}

type loreObservationRequest struct {
	UserID     string  `json:"userId"`
	ResourceID string  `json:"resourceId"`
	Operation  string  `json:"operation"`
	BranchID   string  `json:"branchId"`
	Revision   *string `json:"revision"`
}

func (api *API) InternalPolicyHandler() http.Handler {
	policy := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			writeProblem(writer, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST is supported")
			return
		}
		var input policyRequest
		if !decodeJSON(writer, request, &input) {
			return
		}
		if input.UserID == "" || input.ResourceID == "" || input.Operation == "" {
			writeProblem(writer, http.StatusBadRequest, "invalid_input", "The policy request is incomplete")
			return
		}
		decision, err := api.authorization.CheckPolicy(request.Context(), authz.PolicyCheck{
			UserID: input.UserID, ResourceID: input.ResourceID, Operation: input.Operation,
			BranchID: input.BranchID, BranchName: input.BranchName,
			ProposedRevision: input.ProposedRevision,
		})
		if err != nil {
			writeProblem(writer, http.StatusForbidden, "policy_denied", "The Lore operation is not authorized")
			return
		}
		if !decision.Allowed {
			writeProblem(writer, http.StatusForbidden, "policy_denied", "The Lore operation is not authorized")
			return
		}
		writeJSON(writer, http.StatusOK, map[string]bool{"allowed": true})
	})
	mux := http.NewServeMux()
	mux.Handle("/internal/lore/policy", policy)
	mux.HandleFunc("/internal/lore/observation", api.internalLoreObservation)
	return mux
}

func NewInternalPolicyHandler(store AuthorizationStore) http.Handler {
	return (&API{authorization: store}).InternalPolicyHandler()
}

func (api *API) internalLoreObservation(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeProblem(writer, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST is supported")
		return
	}
	observer, ok := api.authorization.(loreObservationStore)
	if !ok {
		writeProblem(writer, http.StatusServiceUnavailable, "observation_unavailable", "Lore observation is unavailable")
		return
	}
	var input loreObservationRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	if !authz.ValidResourceID(input.ResourceID) || input.UserID == "" || input.BranchID == "" {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "The Lore observation is incomplete")
		return
	}
	loreRepositoryID := strings.TrimPrefix(input.ResourceID, "urc-")
	switch input.Operation {
	case authz.OperationBranchPush:
		if input.Revision == nil || strings.TrimSpace(*input.Revision) == "" {
			writeProblem(writer, http.StatusBadRequest, "invalid_input", "A successful push must include its revision")
			return
		}
		if err := observer.ObserveLoreBranchRevision(
			request.Context(), loreRepositoryID, input.BranchID, strings.TrimSpace(*input.Revision),
		); err != nil {
			writeProblem(writer, http.StatusConflict, "observation_rejected", "The Lore branch state was not observed")
			return
		}
	case authz.OperationBranchDelete:
		if err := observer.DeleteLoreBranchState(request.Context(), loreRepositoryID, input.BranchID); err != nil {
			writeProblem(writer, http.StatusConflict, "observation_rejected", "The Lore branch state was not observed")
			return
		}
	default:
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "The Lore observation operation is invalid")
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (api *API) loreAuthConfirm(writer http.ResponseWriter, request *http.Request) {
	if api.loreAuth == nil {
		writeProblem(
			writer, http.StatusServiceUnavailable, "authentication_unavailable",
			"Lore authentication is unavailable",
		)
		return
	}
	sessionCode := strings.TrimSpace(request.URL.Query().Get("session"))
	if sessionCode == "" && request.Method == http.MethodPost {
		_ = request.ParseForm()
		sessionCode = strings.TrimSpace(request.Form.Get("session"))
	}
	if sessionCode == "" {
		writeProblem(writer, http.StatusBadRequest, "invalid_auth_session", "The Lore authentication session is invalid")
		return
	}
	if request.Method == http.MethodGet {
		user, ok := api.ResolveOptionalActor(writer, request)
		if !ok {
			return
		}
		if user == nil {
			returnTo := "/auth/lore/confirm?session=" + url.QueryEscape(sessionCode)
			http.Redirect(writer, request, "/auth/login?return_to="+url.QueryEscape(returnTo), http.StatusFound)
			return
		}
		api.renderLoreConfirmation(writer, request, sessionCode, "")
		return
	}
	if request.Method != http.MethodPost {
		writeProblem(writer, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET and POST are supported")
		return
	}
	session, _, found, err := api.lookupSession(request)
	if err != nil {
		api.internalError(writer, request, "look up authentication session", err)
		return
	}
	if !found {
		writeProblem(writer, http.StatusUnauthorized, "authentication_required", "Authentication is required")
		return
	}
	if _, err := api.store.ActiveUser(request.Context(), session.UserID); err != nil {
		writeProblem(writer, http.StatusForbidden, "forbidden", "This operation is not permitted")
		return
	}
	csrf := request.Header.Get("X-CSRF-Token")
	if csrf == "" {
		csrf = request.Form.Get("csrfToken")
		request.Header.Set("X-CSRF-Token", csrf)
	}
	if !api.validCSRF(request, session.CSRFDigest) {
		writeProblem(writer, http.StatusForbidden, "csrf_failed", "A valid CSRF token is required")
		return
	}
	if err := api.loreAuth.ConfirmSession(request.Context(), sessionCode, session.UserID); err != nil {
		if errors.Is(err, authz.ErrSessionNotFound) {
			writeProblem(
				writer, http.StatusConflict, "invalid_auth_session",
				"The Lore authentication session is expired or already used",
			)
			return
		}
		api.internalError(writer, request, "confirm Lore authentication session", err)
		return
	}
	api.renderLoreConfirmation(
		writer, request, sessionCode, "Lore access was confirmed. Return to Lore to finish sign-in.",
	)
}

func (api *API) renderLoreConfirmation(
	writer http.ResponseWriter, request *http.Request, sessionCode string, notice string,
) {
	_, sessionToken, found, err := api.lookupSession(request)
	if err != nil || !found {
		writeProblem(writer, http.StatusUnauthorized, "authentication_required", "Authentication is required")
		return
	}
	csrf := api.secrets.CSRFToken(sessionToken)
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Referrer-Policy", "no-referrer")
	noticeHTML := ""
	if notice != "" {
		noticeHTML = "<p>" + html.EscapeString(notice) + "</p>"
	}
	htmlPage := "<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><title>Lore access</title></head><body>" +
		"<main><h1>Allow Lore to access this account?</h1>" + noticeHTML +
		"<p>LoreHub will issue only short-lived repository-scoped access after this confirmation.</p>" +
		"<form id=\"confirm\" method=\"post\"><input type=\"hidden\" name=\"session\" value=\"" +
		html.EscapeString(sessionCode) + "\"><input type=\"hidden\" name=\"csrfToken\" value=\"" +
		html.EscapeString(csrf) + "\"><button type=\"submit\">Allow</button></form>" +
		"<script>document.getElementById('confirm').addEventListener('submit',async function(event){event.preventDefault();" +
		"const form=new FormData(event.target);const response=await fetch(location.href,{method:'POST'," +
		"headers:{'X-CSRF-Token':form.get('csrfToken')},body:new URLSearchParams(form)});document.open();" +
		"document.write(await response.text());document.close();});</script>" +
		"</main></body></html>"
	_, _ = writer.Write([]byte(htmlPage))
}
