package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/lorehub/lorehub/services/api/internal/auth"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
	"github.com/lorehub/lorehub/services/api/internal/runner"
)

type completeRunnerJobRequest struct {
	Conclusion string `json:"conclusion"`
}

func (api *API) runnerClaim(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	_, digest, ok := api.authenticatedRunner(writer, request)
	if !ok {
		return
	}
	job, err := api.runnerControl.RunnerClaimJob(
		request.Context(), digest, api.runnerCredentialKeyID, time.Now().UTC(),
		api.runnerControlConfig.LeaseDuration,
	)
	if err != nil {
		api.runnerControlError(writer, request, "claim runner job", err)
		return
	}
	if job == nil {
		writer.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"job": job, "leaseSeconds": int64(api.runnerControlConfig.LeaseDuration.Seconds()),
	})
}

func (api *API) runnerJobHeartbeat(writer http.ResponseWriter, request *http.Request) {
	runnerRecord, _, ok := api.authenticatedRunner(writer, request)
	if !ok {
		return
	}
	if err := api.runnerControl.RunnerHeartbeatJob(
		request.Context(), request.PathValue("jobID"), runnerRecord.ID,
		api.runnerControlConfig.LeaseDuration,
	); err != nil {
		api.runnerControlError(writer, request, "heartbeat runner job", err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (api *API) runnerJobCancellation(writer http.ResponseWriter, request *http.Request) {
	runnerRecord, _, ok := api.authenticatedRunner(writer, request)
	if !ok {
		return
	}
	requested, err := api.runnerControl.RunnerCancellationRequested(
		request.Context(), request.PathValue("jobID"), runnerRecord.ID,
	)
	if err != nil {
		api.runnerControlError(writer, request, "poll runner job cancellation", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]bool{"requested": requested})
}

func (api *API) runnerJobContext(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	runnerRecord, _, ok := api.authenticatedRunner(writer, request)
	if !ok {
		return
	}
	job, ok := api.runnerLeaseJob(writer, request, runnerRecord.ID)
	if !ok {
		return
	}
	resolved, err := runner.ResolveExecutionContext(
		request.Context(), api.runnerExecutionContext, runner.ExecutionContextRequest{
			Principal: api.runnerControlConfig.Principal, RepositoryID: job.RepositoryID,
			OrganizationID: job.OrganizationID, JobID: job.ID, Environment: job.Environment,
			RequestedScope: "actions:execute",
		},
	)
	if err != nil {
		api.internalError(writer, request, "resolve runner job execution context", err)
		return
	}
	writeJSON(writer, http.StatusOK, resolved)
}

func (api *API) runnerJobToken(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	runnerRecord, _, ok := api.authenticatedRunner(writer, request)
	if !ok {
		return
	}
	job, ok := api.runnerLeaseJob(writer, request, runnerRecord.ID)
	if !ok {
		return
	}
	token, err := runner.IssueJobToken(
		request.Context(), api.runnerJobTokenIssuer, job, api.runnerControlConfig.Principal,
		api.runnerControlConfig.RESTScope, api.runnerControlConfig.GraphQLScope,
	)
	if err != nil {
		api.internalError(writer, request, "issue runner job token", err)
		return
	}
	credentials := runner.JobCredentials{JobToken: token}
	if api.loreCredentials != nil && api.serviceSubjects.ActionsRunner != "" {
		repository := loreclient.RepositoryRef{
			CacheKey: job.RepositoryID, URL: job.LoreURL, LoreRepositoryID: job.LoreRepositoryID,
		}
		partition, err := repository.ValidatedPartition()
		if err != nil {
			api.internalError(writer, request, "validate runner checkout repository", err)
			return
		}
		checkout, err := api.loreCredentials.ForRepository(
			request.Context(), loreclient.CredentialRequest{
				Principal: loreclient.ServicePrincipal(
					loreclient.ServicePurposeActionsRunner, api.serviceSubjects.ActionsRunner,
				),
				Repository: repository, Partition: partition, Scope: loreclient.ScopeRead,
			},
		)
		if err != nil {
			api.internalError(writer, request, "issue runner checkout credential", err)
			return
		}
		encoded := runner.NewCheckoutCredential(checkout)
		credentials.Checkout = &encoded
	}
	writeJSON(writer, http.StatusOK, credentials)
}

func (api *API) runnerJobLogs(writer http.ResponseWriter, request *http.Request) {
	runnerRecord, _, ok := api.authenticatedRunner(writer, request)
	if !ok {
		return
	}
	offset, ok := runnerUploadOffset(writer, request)
	if !ok {
		return
	}
	content, ok := readRunnerUpload(writer, request, api.runnerControlConfig.LogMaxBytes)
	if !ok {
		return
	}
	size, err := api.runnerControl.AppendRunnerJobLog(
		request.Context(), request.PathValue("jobID"), runnerRecord.ID, offset, content,
		api.runnerControlConfig.LogMaxBytes,
	)
	if err != nil {
		api.runnerControlError(writer, request, "upload runner job log", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]int64{"size": size})
}

func (api *API) runnerJobArtifacts(writer http.ResponseWriter, request *http.Request) {
	runnerRecord, _, ok := api.authenticatedRunner(writer, request)
	if !ok {
		return
	}
	offset, ok := runnerUploadOffset(writer, request)
	if !ok {
		return
	}
	content, ok := readRunnerUpload(writer, request, api.runnerControlConfig.ArtifactMaxFileBytes)
	if !ok {
		return
	}
	complete := strings.EqualFold(strings.TrimSpace(request.Header.Get("X-LoreHub-Upload-Complete")), "true")
	size, err := api.runnerControl.AppendRunnerArtifact(
		request.Context(), request.PathValue("jobID"), runnerRecord.ID,
		request.Header.Get("X-LoreHub-Artifact-Name"), offset, content, complete,
		api.runnerControlConfig.ArtifactMaxFileBytes,
		api.runnerControlConfig.ArtifactMaxTotalBytes,
		api.runnerControlConfig.ArtifactMaxCount,
	)
	if err != nil {
		api.runnerControlError(writer, request, "upload runner job artifact", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]int64{"size": size})
}

func (api *API) runnerJobComplete(writer http.ResponseWriter, request *http.Request) {
	runnerRecord, _, ok := api.authenticatedRunner(writer, request)
	if !ok {
		return
	}
	var input completeRunnerJobRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	if !validRunnerConclusion(input.Conclusion) {
		writeProblem(writer, http.StatusBadRequest, "invalid_runner_conclusion", "Runner conclusion is invalid")
		return
	}
	job, ok := api.runnerLeaseJob(writer, request, runnerRecord.ID)
	if !ok {
		return
	}
	if err := api.runnerControl.CompleteJob(
		request.Context(), job, runnerRecord.ID, input.Conclusion, job.LogObjectKey, nil,
	); err != nil {
		api.runnerControlError(writer, request, "complete runner job", err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (api *API) authenticatedRunner(
	writer http.ResponseWriter,
	request *http.Request,
) (platform.Runner, []byte, bool) {
	if api.runners == nil || api.runnerControl == nil || api.runnerSecrets == nil ||
		api.runnerCredentialKeyID == "" || api.runnerControlConfig.LeaseDuration <= 0 {
		writeProblem(writer, http.StatusServiceUnavailable, "runner_control_unavailable",
			"Runner control is unavailable")
		return platform.Runner{}, nil, false
	}
	values := request.Header.Values("Authorization")
	if len(values) != 1 {
		writeRunnerCredentialUnauthorized(writer)
		return platform.Runner{}, nil, false
	}
	fields := strings.Fields(values[0])
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") ||
		!auth.ValidRunnerCredential(fields[1]) || strings.ContainsFunc(fields[1], unicode.IsControl) {
		writeRunnerCredentialUnauthorized(writer)
		return platform.Runner{}, nil, false
	}
	digest := api.runnerSecrets.Digest(fields[1])
	runnerRecord, err := api.runners.AuthenticateRunner(
		request.Context(), digest, api.runnerCredentialKeyID, time.Now().UTC(),
	)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidRunnerToken) {
			writeRunnerCredentialUnauthorized(writer)
			return platform.Runner{}, nil, false
		}
		api.internalError(writer, request, "authenticate runner credential", err)
		return platform.Runner{}, nil, false
	}
	return runnerRecord, digest, true
}

func (api *API) runnerLeaseJob(
	writer http.ResponseWriter,
	request *http.Request,
	runnerID string,
) (runner.Job, bool) {
	job, err := api.runnerControl.RunnerLeaseJob(
		request.Context(), request.PathValue("jobID"), runnerID,
	)
	if err != nil {
		api.runnerControlError(writer, request, "verify runner job lease", err)
		return runner.Job{}, false
	}
	return job, true
}

func (api *API) runnerControlError(
	writer http.ResponseWriter,
	request *http.Request,
	operation string,
	err error,
) {
	switch {
	case errors.Is(err, auth.ErrInvalidRunnerToken):
		writeRunnerCredentialUnauthorized(writer)
	case errors.Is(err, runner.ErrRunnerLeaseNotHeld):
		writeProblem(writer, http.StatusConflict, "runner_lease_not_held", "Runner does not hold this job lease")
	case errors.Is(err, runner.ErrRunnerUploadOffset):
		writeProblem(writer, http.StatusConflict, "runner_upload_offset", "Upload offset does not match stored data")
	case errors.Is(err, runner.ErrRunnerUploadInvalid):
		writeProblem(writer, http.StatusBadRequest, "invalid_runner_upload", "Runner upload is invalid")
	default:
		api.internalError(writer, request, operation, err)
	}
}

func writeRunnerCredentialUnauthorized(writer http.ResponseWriter) {
	writer.Header().Set("WWW-Authenticate", `Bearer realm="runner-control"`)
	writeProblem(writer, http.StatusUnauthorized, "runner_credential_required", "A valid runner credential is required")
}

func runnerUploadOffset(writer http.ResponseWriter, request *http.Request) (int64, bool) {
	value := strings.TrimSpace(request.Header.Get("X-LoreHub-Upload-Offset"))
	if value == "" {
		return 0, true
	}
	offset, err := strconv.ParseInt(value, 10, 64)
	if err != nil || offset < 0 {
		writeProblem(writer, http.StatusBadRequest, "invalid_runner_upload_offset", "Upload offset is invalid")
		return 0, false
	}
	return offset, true
}

func readRunnerUpload(writer http.ResponseWriter, request *http.Request, maximum int64) ([]byte, bool) {
	if maximum <= 0 {
		writeProblem(writer, http.StatusServiceUnavailable, "runner_control_unavailable", "Runner upload is unavailable")
		return nil, false
	}
	request.Body = http.MaxBytesReader(writer, request.Body, maximum)
	content, err := io.ReadAll(request.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeProblem(writer, http.StatusRequestEntityTooLarge, "runner_upload_too_large", "Runner upload is too large")
			return nil, false
		}
		writeProblem(writer, http.StatusBadRequest, "invalid_runner_upload", "Runner upload could not be read")
		return nil, false
	}
	return content, true
}

func validRunnerConclusion(conclusion string) bool {
	switch conclusion {
	case "success", "failure", "cancelled", "timed_out":
		return true
	default:
		return false
	}
}
