package collab

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

const maxRequestBytes = 1 << 20

// Sentinel errors mapped to specific problem codes by handlers.
var (
	ErrPreconditionFailed = errors.New("precondition failed")
	ErrCannotReviewOwn    = errors.New("cannot review own merge request")
)

// writeJSON serializes value as UTF-8 JSON with the given status code.
func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

// writeProblem emits a stable problem+json-style error envelope.
func writeProblem(writer http.ResponseWriter, status int, code string, detail string) {
	writeJSON(writer, status, map[string]any{
		"error": map[string]string{
			"code":   code,
			"detail": detail,
		},
	})
}

// decodeJSON decodes a single JSON value into target, enforcing a 1 MiB limit
// and rejecting unknown fields and trailing content.
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
		writeProblem(writer, http.StatusBadRequest, "invalid_json", "Request body is invalid")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeProblem(writer, http.StatusBadRequest, "invalid_json", "Request body must contain one JSON value")
		return false
	}
	return true
}

// writeLocation sets a Location header for a newly created resource.
func writeLocation(writer http.ResponseWriter, request *http.Request, suffix string) {
	var builder strings.Builder
	builder.WriteString(request.URL.Path)
	if suffix != "" {
		builder.WriteByte('/')
		builder.WriteString(suffix)
	}
	writer.Header().Set("Location", builder.String())
}

// storeError maps a store error to an HTTP problem response. It returns true if
// the error was handled (mapped to a 4xx) and false for internal errors.
func storeError(
	writer http.ResponseWriter,
	request *http.Request,
	operation string,
	err error,
	logger *slog.Logger,
) {
	switch {
	case errors.Is(err, platform.ErrNotFound):
		writeProblem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
	case errors.Is(err, platform.ErrForbidden):
		writeProblem(writer, http.StatusForbidden, "forbidden", "This operation is not permitted")
	case errors.Is(err, platform.ErrConflict):
		writeProblem(writer, http.StatusConflict, "conflict", "The resource already exists")
	case errors.Is(err, ErrPreconditionFailed):
		writeProblem(writer, http.StatusPreconditionFailed, "precondition_failed",
			"The resource was modified since the supplied If-Match value")
	case errors.Is(err, ErrCannotReviewOwn):
		writeProblem(writer, http.StatusUnprocessableEntity, "cannot_review_own",
			"A merge request author cannot review their own request")
	default:
		logger.Error(operation, "error", err, "method", request.Method, "path", request.URL.Path)
		writeProblem(writer, http.StatusInternalServerError, "internal_error",
			"The request could not be completed")
	}
}

// requestContext keeps store calls tied to request cancellation.
func requestContext(request *http.Request) context.Context {
	return request.Context()
}
