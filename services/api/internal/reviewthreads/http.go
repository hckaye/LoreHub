package reviewthreads

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

const maxRequestBytes = (1 << 20) + (32 << 10)

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeProblem(writer http.ResponseWriter, status int, code string, detail string) {
	writeJSON(writer, status, map[string]any{
		"error": map[string]string{"code": code, "detail": detail},
	})
}

func decodeJSON(writer http.ResponseWriter, request *http.Request, target any) bool {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeProblem(writer, http.StatusUnsupportedMediaType, "unsupported_media_type",
			"Content-Type must be application/json")
		return false
	}
	request.Body = http.MaxBytesReader(writer, request.Body, maxRequestBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeProblem(writer, http.StatusRequestEntityTooLarge, "body_too_large", "Request body is too large")
		} else {
			writeProblem(writer, http.StatusBadRequest, "invalid_json", "Request body is invalid")
		}
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeProblem(writer, http.StatusBadRequest, "invalid_json", "Request body must contain one JSON value")
		return false
	}
	return true
}

func (api *API) storeError(
	writer http.ResponseWriter,
	request *http.Request,
	operation string,
	err error,
) {
	switch {
	case errors.Is(err, platform.ErrNotFound), errors.Is(err, loreclient.ErrNotFound):
		writeProblem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
	case errors.Is(err, platform.ErrForbidden):
		writeProblem(writer, http.StatusForbidden, "forbidden", "This operation is not permitted")
	case errors.Is(err, platform.ErrConflict):
		writeProblem(writer, http.StatusConflict, "version_conflict",
			"The pull request or review comment changed before this request")
	case errors.Is(err, ErrInvalidInput):
		writeProblem(writer, http.StatusUnprocessableEntity, "invalid_review_line",
			"The review comment is not valid for the selected line")
	case errors.Is(err, loreclient.ErrCredentialUnavailable),
		errors.Is(err, loreclient.ErrCredentialIssuerRequired):
		writeProblem(writer, http.StatusServiceUnavailable, "lore_unavailable", "Lore review validation is unavailable")
	default:
		api.logger.Error(operation, "error", err, "method", request.Method, "path", request.URL.Path)
		writeProblem(writer, http.StatusInternalServerError, "internal_error", "The request could not be completed")
	}
}
