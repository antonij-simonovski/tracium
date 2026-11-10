package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/tracium/api/internal/release"
)

// ReleaseReport handles GET /v1/metrics/workflows/{name}/versions/{version}/report.
func (h *MetricsHandler) ReleaseReport(w http.ResponseWriter, r *http.Request) {
	scope, ok := resolveWorkspaceScope(w, r, h.access, r.URL.Query().Get("workspace_id"))
	if !ok {
		return
	}
	report, err := h.reports.Get(r.Context(), scope, chi.URLParam(r, "name"), chi.URLParam(r, "version"))
	if errors.Is(err, release.ErrNotFound) {
		respondError(w, http.StatusNotFound, "REPORT_NOT_FOUND", "release report not found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to load release report")
		return
	}
	respondJSON(w, http.StatusOK, report)
}

// ReleaseReports handles GET /v1/release-reports, newest first.
func (h *MetricsHandler) ReleaseReports(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	status := q.Get("status")
	if status != "" && status != release.StatusPending && status != release.StatusFinal {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "status must be pending or final")
		return
	}
	limit := 20
	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 100 {
			respondError(w, http.StatusBadRequest, "BAD_REQUEST", "limit must be between 1 and 100")
			return
		}
		limit = n
	}
	scope, ok := resolveWorkspaceScope(w, r, h.access, q.Get("workspace_id"))
	if !ok {
		return
	}
	reports, err := h.reports.List(r.Context(), scope, status, limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to list release reports")
		return
	}
	respondPage(w, reports, len(reports), 1, limit)
}
