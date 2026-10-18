package handler

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/internal/query"
)

// SpanHandler handles span-related endpoints.
type SpanHandler struct {
	repo   query.TraceRepository
	access WorkspaceAccess
}

// NewSpanHandler constructs a SpanHandler with the given repository.
func NewSpanHandler(repo query.TraceRepository, access WorkspaceAccess) *SpanHandler {
	return &SpanHandler{repo: repo, access: access}
}

// ListSpans handles GET /v1/traces/{traceId}/spans.
func (h *SpanHandler) ListSpans(w http.ResponseWriter, r *http.Request) {
	traceID := chi.URLParam(r, "traceId")
	if traceID == "" {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "traceId is required")
		return
	}

	scope, ok := resolveWorkspaceScope(w, r, h.access, r.URL.Query().Get("workspace_id"))
	if !ok {
		return
	}
	spans, err := h.repo.GetSpans(r.Context(), traceID, scope)
	if err != nil {
		if errors.Is(err, query.ErrNotFound) {
			respondError(w, http.StatusNotFound, "TRACE_NOT_FOUND", "trace not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to list spans")
		return
	}

	// Never serve a row a newer collector wrote: its fields may mean something else.
	if err := model.CheckSchemaCompatibility(spans); err != nil {
		respondError(w, http.StatusInternalServerError, "SCHEMA_INCOMPATIBLE", err.Error())
		return
	}

	respondPage(w, spans, len(spans), 1, len(spans))
}
