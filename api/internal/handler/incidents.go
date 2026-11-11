package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/tracium/api/internal/anomaly"
	"github.com/tracium/api/internal/incident"
	"github.com/tracium/api/internal/middleware"
	"github.com/tracium/api/internal/model"
)

// IncidentHandler serves stored incidents and their mutes. An incident in a
// workspace the caller cannot read answers 404.
type IncidentHandler struct {
	store    incident.Store
	access   WorkspaceAccess
	notifier incident.Notifier
	now      func() time.Time
}

func NewIncidentHandler(store incident.Store, access WorkspaceAccess, notifier incident.Notifier) *IncidentHandler {
	if notifier == nil {
		notifier = incident.NopNotifier{}
	}
	return &IncidentHandler{store: store, access: access, notifier: notifier, now: time.Now}
}

type incidentPage struct {
	model.Page
	LastEvaluatedAt *time.Time `json:"last_evaluated_at"`
}

var muteDurations = map[string]time.Duration{"24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour, "forever": 0}

// List handles GET /v1/incidents, open incidents first, newest first.
func (h *IncidentHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var status incident.Status
	switch s := q.Get("status"); s {
	case "", "all":
	case "open", "resolved":
		status = incident.Status(s)
	default:
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "status must be open, resolved or all")
		return
	}
	page, pageSize := 1, 20
	for key, dst := range map[string]*int{"page": &page, "page_size": &pageSize} {
		if s := q.Get(key); s != "" {
			n, err := strconv.Atoi(s)
			if err != nil || n < 1 || (key == "page_size" && n > 100) {
				respondError(w, http.StatusBadRequest, "BAD_REQUEST", key+" is out of range")
				return
			}
			*dst = n
		}
	}
	scope, ok := resolveWorkspaceScope(w, r, h.access, q.Get("workspace_id"))
	if !ok {
		return
	}
	items, total, err := h.store.List(r.Context(), scope, status, page, pageSize)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to list incidents")
		return
	}
	last, err := h.store.LastEvaluated(r.Context(), scope)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to list incidents")
		return
	}
	respondJSON(w, http.StatusOK, incidentPage{model.Page{Items: items, Total: total, Page: page, PageSize: pageSize}, last})
}

// get loads the {id} incident within the caller's workspaces, answering the request on failure.
func (h *IncidentHandler) get(w http.ResponseWriter, r *http.Request) (*incident.Incident, bool) {
	scope, ok := resolveWorkspaceScope(w, r, h.access, "")
	if !ok {
		return nil, false
	}
	inc, err := h.store.Get(r.Context(), scope, chi.URLParam(r, "id"))
	if errors.Is(err, incident.ErrNotFound) {
		respondError(w, http.StatusNotFound, "INCIDENT_NOT_FOUND", "incident not found")
		return nil, false
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to load incident")
		return nil, false
	}
	return inc, true
}

// Get handles GET /v1/incidents/{id}.
func (h *IncidentHandler) Get(w http.ResponseWriter, r *http.Request) {
	if inc, ok := h.get(w, r); ok {
		respondJSON(w, http.StatusOK, inc)
	}
}

// resolve resolves inc as the caller; an incident already resolved is left as it is.
func (h *IncidentHandler) resolve(w http.ResponseWriter, r *http.Request, inc *incident.Incident) bool {
	if inc.Status != incident.StatusOpen {
		return true
	}
	principal, _ := middleware.PrincipalFromContext(r.Context())
	at := h.now().UTC()
	err := h.store.Resolve(r.Context(), inc.ID, at, principal.UserID)
	if err != nil && !errors.Is(err, incident.ErrNotFound) {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to resolve incident")
		return false
	}
	if err == nil {
		inc.Status, inc.ResolvedAt, inc.ResolvedBy, inc.UpdatedAt = incident.StatusResolved, &at, principal.UserID, at
	}
	return true
}

// Resolve handles POST /v1/incidents/{id}/resolve and emits a resolved event.
func (h *IncidentHandler) Resolve(w http.ResponseWriter, r *http.Request) {
	inc, ok := h.get(w, r)
	if !ok {
		return
	}
	wasOpen := inc.Status == incident.StatusOpen
	if !h.resolve(w, r, inc) {
		return
	}
	if wasOpen && inc.Status == incident.StatusResolved {
		if err := h.notifier.Notify(r.Context(), incident.Event{Type: incident.EventResolved, Incident: *inc}); err != nil {
			log.Printf("incident %s: notify resolved: %v", inc.ID, err)
		}
	}
	respondJSON(w, http.StatusOK, inc)
}

type muteRequest struct {
	Duration string `json:"duration"`
	Metric   string `json:"metric"`
}

// Mute handles POST /v1/incidents/{id}/mute: it mutes the incident's subject
// (one metric of it when metric is set) and resolves the incident without notifying.
func (h *IncidentHandler) Mute(w http.ResponseWriter, r *http.Request) {
	var req muteRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondDecodeError(w, err)
		return
	}
	d, valid := muteDurations[req.Duration]
	if !valid {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "duration must be 24h, 7d or forever")
		return
	}
	finding, isRelease := strings.CutPrefix(req.Metric, incident.ReleaseMetric+":")
	if req.Metric != "" && req.Metric != "budget" && !validAnomalyMetrics[req.Metric] && req.Metric != incident.ReleaseMetric && !(isRelease && anomaly.FindingMetrics[finding]) && req.Metric != incident.AccuracyMetric {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "metric must be empty, budget, "+incident.ReleaseMetric+", "+incident.ReleaseMetric+":<finding>, "+incident.AccuracyMetric+" or one of "+anomalyMetricList)
		return
	}
	inc, ok := h.get(w, r)
	if !ok {
		return
	}
	principal, _ := middleware.PrincipalFromContext(r.Context())
	m := incident.Mute{WorkspaceID: inc.WorkspaceID, Scope: inc.Scope, Subject: inc.Subject, Metric: req.Metric, CreatedBy: principal.UserID}
	if d > 0 {
		until := h.now().UTC().Add(d)
		m.Until = &until
	}
	if err := h.store.AddMute(r.Context(), &m); err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to mute incident")
		return
	}
	if !h.resolve(w, r, inc) {
		return
	}
	respondJSON(w, http.StatusCreated, m)
}

// ListMutes handles GET /v1/workspaces/{id}/incident-mutes, active mutes only.
func (h *IncidentHandler) ListMutes(w http.ResponseWriter, r *http.Request) {
	scope, ok := resolveWorkspaceScope(w, r, h.access, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	mutes, err := h.store.Mutes(r.Context(), scope[0])
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to list mutes")
		return
	}
	respondPage(w, mutes, len(mutes), 1, len(mutes))
}

// DeleteMute handles DELETE /v1/workspaces/{id}/incident-mutes/{muteId}.
func (h *IncidentHandler) DeleteMute(w http.ResponseWriter, r *http.Request) {
	scope, ok := resolveWorkspaceScope(w, r, h.access, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	err := h.store.DeleteMute(r.Context(), scope[0], chi.URLParam(r, "muteId"))
	if errors.Is(err, incident.ErrNotFound) {
		respondError(w, http.StatusNotFound, "MUTE_NOT_FOUND", "mute not found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to delete mute")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
