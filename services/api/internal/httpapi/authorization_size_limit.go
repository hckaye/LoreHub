package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/lorehub/lorehub/services/api/internal/authz"
)

func (api *API) denyOversizedBranchPush(writer http.ResponseWriter, request *http.Request, input policyRequest) bool {
	if !isBranchPushPolicy(input) || input.RevisionTreeSize == nil {
		return false
	}
	limit, err := api.effectiveMaxRepositorySizeBytes(request.Context())
	if err != nil {
		api.logLorePolicyDecision(input, false, err)
		writeProblem(writer, http.StatusInternalServerError, "internal_error", "The request could not be completed")
		return true
	}
	if limit <= 0 {
		return false
	}
	if *input.RevisionTreeSize <= uint64(limit) {
		return false
	}
	api.logLorePolicyDecision(input, false, nil)
	writeJSON(writer, http.StatusForbidden, map[string]any{
		"allowed": false,
		"code":    "repository_size_limit",
		"message": repositorySizeLimitMessage(limit, *input.RevisionTreeSize),
	})
	return true
}

type maxRepositorySizeResolver interface {
	EffectiveMaxRepositorySizeBytes(context.Context) (int64, error)
}

func (api *API) effectiveMaxRepositorySizeBytes(ctx context.Context) (int64, error) {
	if resolver, ok := api.authorization.(maxRepositorySizeResolver); ok {
		return resolver.EffectiveMaxRepositorySizeBytes(ctx)
	}
	if api.instanceSettings != nil {
		override, err := api.instanceSettings.GetMaxRepositorySizeBytesOverride(ctx)
		if err != nil {
			return 0, err
		}
		if override != nil {
			return *override, nil
		}
	}
	return api.maxRepositorySizeBytes, nil
}

func isBranchPushPolicy(input policyRequest) bool {
	return input.Operation == authz.OperationBranchPush ||
		input.HookPoint == "BranchPush" || input.HookPoint == authz.OperationBranchPush
}

func repositorySizeLimitMessage(limitBytes int64, pushedBytes uint64) string {
	return fmt.Sprintf(
		"The repository size limit is %.1f MB; the pushed revision is %.1f MB",
		float64(limitBytes)/(1024*1024),
		float64(pushedBytes)/(1024*1024),
	)
}
