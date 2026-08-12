package collab

import (
	"net/http"
	"strings"
)

type branchRuleRequest struct {
	Pattern              string   `json:"pattern"`
	RequiredApprovals    int      `json:"requiredApprovals"`
	RequireCISuccess     bool     `json:"requireCiSuccess"`
	RequiredStatusChecks []string `json:"requiredStatusChecks"`
	BlockDirectPush      bool     `json:"blockDirectPush"`
}

func (api *API) listBranchRules(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.optionalActor(writer, request)
	if !ok {
		return
	}
	repo, ok := api.lookup(writer, request, actor)
	if !ok {
		return
	}
	rules, err := api.store.ListBranchRules(requestContext(request), repo.ID)
	if err != nil {
		storeError(writer, request, "list branch rules", err, api.logger)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": rules})
}

func (api *API) createBranchRule(writer http.ResponseWriter, request *http.Request) {
	actor, repo, ok := api.requireMutationActor(writer, request)
	if !ok {
		return
	}
	access, ok := api.permission(writer, request, actor, repo)
	if !ok {
		return
	}
	if !access.CanManageBranchRules() {
		writeProblem(writer, http.StatusForbidden, "forbidden", "This operation is not permitted")
		return
	}
	input, ok := decodeBranchRuleRequest(writer, request)
	if !ok {
		return
	}
	rule, err := api.store.CreateBranchRule(requestContext(request), actor, repo.ID, input)
	if err != nil {
		storeError(writer, request, "create branch rule", err, api.logger)
		return
	}
	writeLocation(writer, request, rule.ID)
	writeJSON(writer, http.StatusCreated, rule)
}

func (api *API) patchBranchRule(writer http.ResponseWriter, request *http.Request) {
	actor, repo, ok := api.requireMutationActor(writer, request)
	if !ok {
		return
	}
	ruleID := strings.TrimSpace(request.PathValue("ruleID"))
	if ruleID == "" {
		writeProblem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
		return
	}
	input, ok := decodeBranchRuleRequest(writer, request)
	if !ok {
		return
	}
	rule, err := api.store.UpdateBranchRule(requestContext(request), actor, repo.ID, ruleID, input)
	if err != nil {
		storeError(writer, request, "update branch rule", err, api.logger)
		return
	}
	writeJSON(writer, http.StatusOK, rule)
}

func (api *API) deleteBranchRule(writer http.ResponseWriter, request *http.Request) {
	actor, repo, ok := api.requireMutationActor(writer, request)
	if !ok {
		return
	}
	ruleID := strings.TrimSpace(request.PathValue("ruleID"))
	if ruleID == "" {
		writeProblem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
		return
	}
	if err := api.store.DeleteBranchRule(requestContext(request), actor, repo.ID, ruleID); err != nil {
		storeError(writer, request, "delete branch rule", err, api.logger)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func decodeBranchRuleRequest(writer http.ResponseWriter, request *http.Request) (BranchRuleInput, bool) {
	var body branchRuleRequest
	if !decodeJSON(writer, request, &body) {
		return BranchRuleInput{}, false
	}
	input, err := validateBranchRuleInput(BranchRuleInput{
		Pattern:              body.Pattern,
		RequiredApprovals:    body.RequiredApprovals,
		RequireCISuccess:     body.RequireCISuccess,
		RequiredStatusChecks: body.RequiredStatusChecks,
		BlockDirectPush:      body.BlockDirectPush,
	})
	if err != nil {
		validationError(writer, err)
		return BranchRuleInput{}, false
	}
	return input, true
}
