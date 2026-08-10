package httpapi

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/lorehub/lorehub/services/api/internal/runner"
)

const (
	maxSARIFRequestBody    = 14 << 20
	maxSARIFUploadList     = 100
	maxCodeScanningAlerts  = 1000
	maxSARIFCheckoutURI    = 4096
	maxSARIFToolName       = 255
	maxSARIFExpectedRef    = 255
	maxSARIFExpectedCommit = 128
)

var (
	errSARIFPayloadInvalid  = errors.New("SARIF request payload is invalid")
	errSARIFPayloadTooLarge = errors.New("SARIF request payload is too large")
)

type sarifUploadRequest struct {
	CommitSHA   string `json:"commit_sha"`
	Ref         string `json:"ref"`
	SARIF       string `json:"sarif"`
	CheckoutURI string `json:"checkout_uri,omitempty"`
	ToolName    string `json:"tool_name,omitempty"`
}

func (api *API) uploadSARIF(writer http.ResponseWriter, request *http.Request) {
	if api.actionsSecurity == nil || api.actionsJobTokens == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "code_scanning_unavailable",
			"Code scanning is not configured")
		return
	}
	rawToken, ok := jobBearerToken(writer, request)
	if !ok {
		return
	}
	verified, err := api.actionsJobTokens.Verify(
		request.Context(), rawToken, runner.JobTokenRESTScope, runner.JobTokenGraphQLScope,
	)
	if err != nil {
		api.jobTokenError(writer, request, err)
		return
	}
	var input sarifUploadRequest
	if !decodeSARIFUploadRequest(writer, request, &input) {
		return
	}
	if err := validateSARIFUploadRequest(input); err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_sarif_request", err.Error())
		return
	}
	document, err := decodeSARIFDocument(input.SARIF)
	if err != nil {
		if errors.Is(err, errSARIFPayloadTooLarge) {
			writeProblem(writer, http.StatusRequestEntityTooLarge, "sarif_too_large",
				"The decoded SARIF document exceeds 10 MiB")
			return
		}
		writeProblem(writer, http.StatusBadRequest, "invalid_sarif", "The SARIF payload is invalid")
		return
	}
	claims := verified.Claims
	upload, err := api.actionsSecurity.UploadSARIF(request.Context(), runner.SARIFUploadInput{
		Claims: runner.SARIFJobClaims{
			RepositoryID: claims.RepositoryID,
			RunID:        claims.RunID,
			JobID:        claims.JobID,
			Attempt:      claims.Attempt,
		},
		Owner:            request.PathValue("owner"),
		Repository:       request.PathValue("repository"),
		ExpectedRevision: input.CommitSHA,
		ExpectedRef:      input.Ref,
		Document:         document,
	})
	if err != nil {
		api.sarifError(writer, request, "upload SARIF", err)
		return
	}
	location := sarifUploadLocation(
		request.PathValue("owner"), request.PathValue("repository"), upload.ID,
	)
	writer.Header().Set("Location", location)
	writeJSON(writer, http.StatusAccepted, map[string]string{"id": upload.ID, "url": location})
}

func (api *API) listSARIFUploads(writer http.ResponseWriter, request *http.Request) {
	selector, ok := api.codeScanningReadSelector(writer, request)
	if !ok {
		return
	}
	limit, err := codeScanningLimit(request, maxSARIFUploadList)
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_code_scanning_limit", err.Error())
		return
	}
	uploads, err := api.actionsSecurity.ListSARIFUploads(request.Context(), selector, limit)
	if err != nil {
		api.sarifError(writer, request, "list SARIF uploads", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"sarifUploads": uploads})
}

func (api *API) getSARIFUpload(writer http.ResponseWriter, request *http.Request) {
	selector, ok := api.codeScanningReadSelector(writer, request)
	if !ok {
		return
	}
	uploadID := request.PathValue("uploadID")
	if !validCodeScanningID(uploadID) {
		writeProblem(writer, http.StatusNotFound, "sarif_not_found", "The SARIF resource was not found")
		return
	}
	upload, err := api.actionsSecurity.GetSARIFUpload(
		request.Context(), selector, uploadID,
	)
	if err != nil {
		api.sarifError(writer, request, "get SARIF upload", err)
		return
	}
	writeJSON(writer, http.StatusOK, upload)
}

func (api *API) listCodeScanningAlerts(writer http.ResponseWriter, request *http.Request) {
	selector, ok := api.codeScanningReadSelector(writer, request)
	if !ok {
		return
	}
	limit, err := codeScanningLimit(request, maxCodeScanningAlerts)
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_code_scanning_limit", err.Error())
		return
	}
	uploadID := request.URL.Query().Get("upload_id")
	if uploadID != "" && !validCodeScanningID(uploadID) {
		writeProblem(writer, http.StatusBadRequest, "invalid_sarif_upload_id",
			"The SARIF upload ID is invalid")
		return
	}
	alerts, err := api.actionsSecurity.ListCodeScanningAlerts(
		request.Context(), selector, uploadID, limit,
	)
	if err != nil {
		api.sarifError(writer, request, "list code scanning alerts", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"alerts": alerts})
}

func (api *API) codeScanningReadSelector(
	writer http.ResponseWriter,
	request *http.Request,
) (runner.SARIFRepositorySelector, bool) {
	if api.actionsSecurity == nil || api.actions == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "code_scanning_unavailable",
			"Code scanning is not configured")
		return runner.SARIFRepositorySelector{}, false
	}
	actor, ok := api.ResolveOptionalActor(writer, request)
	if !ok {
		return runner.SARIFRepositorySelector{}, false
	}
	actorID := ""
	if actor != nil {
		actorID = actor.ID
	}
	owner := request.PathValue("owner")
	repository := request.PathValue("repository")
	access, err := api.actions.RepositoryForActions(request.Context(), owner, repository, actorID)
	if err != nil {
		api.actionsError(writer, request, "check code scanning read permission", err)
		return runner.SARIFRepositorySelector{}, false
	}
	if !access.CanRead {
		writeProblem(writer, http.StatusNotFound, "sarif_not_found", "The SARIF resource was not found")
		return runner.SARIFRepositorySelector{}, false
	}
	return runner.SARIFRepositorySelector{
		RepositoryID: access.ID,
		Owner:        owner,
		Repository:   repository,
	}, true
}

func jobBearerToken(writer http.ResponseWriter, request *http.Request) (string, bool) {
	values := request.Header.Values("Authorization")
	if len(values) != 1 {
		writeJobTokenRequired(writer)
		return "", false
	}
	fields := strings.Fields(values[0])
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") ||
		fields[1] == "" || strings.ContainsFunc(fields[1], unicode.IsControl) {
		writeJobTokenRequired(writer)
		return "", false
	}
	return fields[1], true
}

func writeJobTokenRequired(writer http.ResponseWriter) {
	writer.Header().Set("WWW-Authenticate", "Bearer")
	writeProblem(writer, http.StatusUnauthorized, "actions_job_token_required",
		"A valid Actions job token is required")
}

func decodeSARIFUploadRequest(
	writer http.ResponseWriter,
	request *http.Request,
	target *sarifUploadRequest,
) bool {
	request.Body = http.MaxBytesReader(writer, request.Body, maxSARIFRequestBody)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeProblem(writer, http.StatusRequestEntityTooLarge, "sarif_request_too_large",
				"The SARIF request body exceeds 14 MiB")
			return false
		}
		writeProblem(writer, http.StatusBadRequest, "invalid_json", "Request body is invalid")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeProblem(writer, http.StatusBadRequest, "invalid_json", "Request body must contain one JSON value")
		return false
	}
	return true
}

func validateSARIFUploadRequest(input sarifUploadRequest) error {
	if !validSARIFRequestValue(input.CommitSHA, maxSARIFExpectedCommit) ||
		strings.ContainsAny(input.CommitSHA, "\x00\r\n\t ") {
		return errors.New("commit_sha is required and must be a valid Lore revision")
	}
	branch := strings.TrimPrefix(input.Ref, "refs/heads/")
	if !validSARIFRequestValue(input.Ref, maxSARIFExpectedRef) ||
		!strings.HasPrefix(input.Ref, "refs/heads/") || branch == "" ||
		strings.ContainsAny(input.Ref, "\x00\r\n\t \\") || strings.Contains(input.Ref, "..") ||
		strings.Contains(input.Ref, "//") || strings.Contains(input.Ref, "@{") ||
		strings.HasSuffix(input.Ref, "/") || strings.HasSuffix(input.Ref, ".") {
		return errors.New("ref is required and must use refs/heads/<branch>")
	}
	for _, segment := range strings.Split(branch, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return errors.New("ref contains an invalid branch segment")
		}
	}
	if input.SARIF == "" {
		return errors.New("sarif is required")
	}
	if input.ToolName != "" && !validSARIFRequestValue(input.ToolName, maxSARIFToolName) {
		return errors.New("tool_name is invalid")
	}
	if input.CheckoutURI != "" {
		if !validSARIFRequestValue(input.CheckoutURI, maxSARIFCheckoutURI) {
			return errors.New("checkout_uri is invalid")
		}
		parsed, err := url.Parse(input.CheckoutURI)
		if err != nil || parsed.Scheme == "" {
			return errors.New("checkout_uri must be an absolute URI")
		}
	}
	return nil
}

func validSARIFRequestValue(value string, maximum int) bool {
	return value != "" && len(value) <= maximum && strings.TrimSpace(value) == value &&
		!strings.ContainsFunc(value, unicode.IsControl)
}

func decodeSARIFDocument(encoded string) ([]byte, error) {
	if strings.ContainsFunc(encoded, func(character rune) bool {
		return unicode.IsSpace(character) || unicode.IsControl(character)
	}) {
		return nil, errSARIFPayloadInvalid
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(decoded) == 0 {
		return nil, errSARIFPayloadInvalid
	}
	if len(decoded) >= 2 && decoded[0] == 0x1f && decoded[1] == 0x8b {
		reader, err := gzip.NewReader(bytes.NewReader(decoded))
		if err != nil {
			return nil, errSARIFPayloadInvalid
		}
		document, readErr := io.ReadAll(io.LimitReader(reader, runner.MaxSARIFUploadBytes+1))
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil {
			return nil, errSARIFPayloadInvalid
		}
		if len(document) > runner.MaxSARIFUploadBytes {
			return nil, errSARIFPayloadTooLarge
		}
		return document, nil
	}
	if len(decoded) > runner.MaxSARIFUploadBytes {
		return nil, errSARIFPayloadTooLarge
	}
	return decoded, nil
}

func codeScanningLimit(request *http.Request, maximum int) (int, error) {
	values := request.URL.Query()["limit"]
	if len(values) == 0 {
		values = request.URL.Query()["per_page"]
	}
	if len(values) == 0 {
		return 0, nil
	}
	if len(values) != 1 || values[0] == "" {
		return 0, errors.New("limit must be supplied exactly once")
	}
	limit, err := strconv.Atoi(values[0])
	if err != nil || limit <= 0 || limit > maximum {
		return 0, fmt.Errorf("limit must be between 1 and %d", maximum)
	}
	return limit, nil
}

func sarifUploadLocation(owner string, repository string, uploadID string) string {
	return "/api/v1/repositories/" + url.PathEscape(owner) + "/" + url.PathEscape(repository) +
		"/code-scanning/sarif-uploads/" + url.PathEscape(uploadID)
}

func validCodeScanningID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value
}

func (api *API) jobTokenError(
	writer http.ResponseWriter,
	request *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, runner.ErrActionsJobTokenInvalid):
		writeJobTokenRequired(writer)
	case errors.Is(err, runner.ErrActionsJobTokenScope),
		errors.Is(err, runner.ErrActionsJobTokenUnauthorized):
		writeProblem(writer, http.StatusForbidden, "actions_job_token_forbidden",
			"The Actions job token is not authorized for this operation")
	default:
		api.logger.Error(
			"verify Actions job token",
			"error_type", fmt.Sprintf("%T", err),
			"method", request.Method,
			"path", request.URL.Path,
		)
		writeProblem(writer, http.StatusInternalServerError, "internal_error",
			"The request could not be completed")
	}
}

func (api *API) sarifError(
	writer http.ResponseWriter,
	request *http.Request,
	operation string,
	err error,
) {
	switch {
	case errors.Is(err, runner.ErrSARIFNotFound):
		writeProblem(writer, http.StatusNotFound, "sarif_not_found", "The SARIF resource was not found")
	case errors.Is(err, runner.ErrSARIFBoundary):
		writeProblem(writer, http.StatusConflict, "sarif_boundary_conflict",
			"The SARIF upload does not match the current Actions job boundary")
	case errors.Is(err, runner.ErrSARIFTooLarge):
		writeProblem(writer, http.StatusRequestEntityTooLarge, "sarif_too_large",
			"The decoded SARIF document exceeds 10 MiB")
	case errors.Is(err, runner.ErrSARIFInvalid):
		writeProblem(writer, http.StatusUnprocessableEntity, "invalid_sarif",
			"The SARIF document is invalid")
	case errors.Is(err, runner.ErrSARIFListLimit):
		writeProblem(writer, http.StatusBadRequest, "invalid_code_scanning_limit",
			"The code scanning result limit is invalid")
	default:
		api.internalError(writer, request, operation, err)
	}
}
