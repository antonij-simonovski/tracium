package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/internal/query"
)

// TraceHandler handles all trace-related endpoints.
type TraceHandler struct {
	repo   query.TraceRepository
	access WorkspaceAccess
}

// NewTraceHandler constructs a TraceHandler with the given repository and
// workspace access resolver (used to scope listings to the caller's workspaces).
func NewTraceHandler(repo query.TraceRepository, access WorkspaceAccess) *TraceHandler {
	return &TraceHandler{repo: repo, access: access}
}

// ListTraces handles GET /v1/traces.
func (h *TraceHandler) ListTraces(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := query.TraceFilter{
		UserID:   q.Get("user_id"),
		Model:    q.Get("model"),
		Workflow: q.Get("workflow"),
		Version:  q.Get("version"),
	}

	scope, ok := resolveWorkspaceScope(w, r, h.access, q.Get("workspace_id"))
	if !ok {
		return
	}
	filter.WorkspaceIDs = scope

	rng := q.Get("range")
	if rng == "" {
		rng = "30d"
	}
	win, err := query.ParseRange(rng, time.Now())
	if err != nil {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	filter.StartAfter, filter.StartBefore = win.Start, win.End

	if hasErrorStr := q.Get("has_error"); hasErrorStr != "" {
		b, err := strconv.ParseBool(hasErrorStr)
		if err != nil {
			respondError(w, http.StatusBadRequest, "BAD_REQUEST", "has_error must be a boolean")
			return
		}
		filter.HasError = &b
	}

	if pageStr := q.Get("page"); pageStr != "" {
		p, err := strconv.Atoi(pageStr)
		if err != nil || p < 1 {
			respondError(w, http.StatusBadRequest, "BAD_REQUEST", "page must be a positive integer")
			return
		}
		filter.Page = p
	}

	if pageSizeStr := q.Get("page_size"); pageSizeStr != "" {
		ps, err := strconv.Atoi(pageSizeStr)
		if err != nil || ps < 1 {
			respondError(w, http.StatusBadRequest, "BAD_REQUEST", "page_size must be a positive integer")
			return
		}
		filter.PageSize = ps
	}

	if err := filter.Validate(); err != nil {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}

	traces, total, err := h.repo.ListTraces(r.Context(), filter)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to list traces")
		return
	}

	respondPage(w, traces, int(total), filter.Page, filter.PageSize)
}

// GetTrace handles GET /v1/traces/{id}, returning the trace with its spans.
func (h *TraceHandler) GetTrace(w http.ResponseWriter, r *http.Request) {
	traceID := chi.URLParam(r, "id")
	if traceID == "" {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "trace id is required")
		return
	}

	scope, ok := resolveWorkspaceScope(w, r, h.access, r.URL.Query().Get("workspace_id"))
	if !ok {
		return
	}
	trace, err := h.repo.GetTrace(r.Context(), traceID, scope)
	if err != nil {
		if errors.Is(err, query.ErrNotFound) {
			respondError(w, http.StatusNotFound, "TRACE_NOT_FOUND", "trace not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	spans, err := h.repo.GetSpans(r.Context(), traceID, scope)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	if err := model.CheckSchemaCompatibility(spans); err != nil {
		respondError(w, http.StatusInternalServerError, "SCHEMA_INCOMPATIBLE", err.Error())
		return
	}

	respondJSON(w, http.StatusOK, model.TraceDetail{Trace: *trace, Spans: spans})
}
