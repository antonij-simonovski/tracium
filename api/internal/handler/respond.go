package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/tracium/api/internal/model"
)

// respondJSON writes v as JSON with the given HTTP status code.
// Always use this instead of calling json.NewEncoder directly in handlers.
func respondJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// respondError writes a standard ErrorResponse JSON body with the given status.
func respondError(w http.ResponseWriter, status int, code, message string) {
	respondJSON(w, status, model.ErrorResponse{Code: code, Message: message})
}

// respondDecodeError answers a request body that failed to decode: 413 when it
// exceeded its http.MaxBytesReader cap, 400 otherwise.
func respondDecodeError(w http.ResponseWriter, err error) {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		respondError(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "request body is too large")
		return
	}
	respondError(w, http.StatusBadRequest, "BAD_REQUEST", "request body must be valid JSON")
}

// respondPage writes a 200 with the standard paginated list envelope.
func respondPage(w http.ResponseWriter, items any, total, page, pageSize int) {
	respondJSON(w, http.StatusOK, model.Page{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}
