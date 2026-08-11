package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

const maxAuditSearchLength = 200

func (api *API) organizationAuditLog(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "private, no-store")
	if api.identityStore == nil {
		api.identityUnavailable(writer)
		return
	}
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	values := request.URL.Query()
	if duplicateAuditQuery(values) {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "Audit log parameters must be unique")
		return
	}
	query := strings.TrimSpace(values.Get("query"))
	if !validAuditSearch(query) {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "The audit log search is invalid")
		return
	}
	limit, err := queryLimit(values.Get("perPage"), 50, 100)
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "The audit log page size is invalid")
		return
	}
	page, err := api.identityStore.OrganizationAuditLog(
		request.Context(), actor, request.PathValue("organization"),
		query, values.Get("before"), limit,
	)
	if errors.Is(err, platform.ErrInvalidAuditCursor) {
		writeProblem(writer, http.StatusBadRequest, "invalid_cursor", "The audit log cursor is invalid")
		return
	}
	if err != nil {
		api.platformError(writer, request, "list organization audit log", err)
		return
	}
	writeJSON(writer, http.StatusOK, page)
}

func duplicateAuditQuery(values map[string][]string) bool {
	for _, key := range []string{"before", "perPage", "query"} {
		if len(values[key]) > 1 {
			return true
		}
	}
	return false
}

func validAuditSearch(value string) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxAuditSearchLength {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
