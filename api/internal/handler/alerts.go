package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/tracium/api/internal/alert"
	"github.com/tracium/api/internal/incident"
)

// AlertHandler manages a workspace's alert channels. Members can list them;
// only the owner can change or test them. URL targets are always masked.
type AlertHandler struct {
	store      alert.Store
	rules      alert.RuleStore
	dispatcher *alert.Dispatcher
	incidents  incident.Store
	access     WorkspaceAccess
	owners     OwnerCheck
}

func NewAlertHandler(store alert.Store, rules alert.RuleStore, dispatcher *alert.Dispatcher, incidents incident.Store, access WorkspaceAccess, owners OwnerCheck) *AlertHandler {
	return &AlertHandler{store: store, rules: rules, dispatcher: dispatcher, incidents: incidents, access: access, owners: owners}
}

type alertChannelView struct {
	alert.Channel
	Secret string `json:"secret,omitempty"`
}

func viewChannel(c alert.Channel) alertChannelView {
	c.Target = alert.Mask(c.Kind, c.Target)
	return alertChannelView{Channel: c}
}

var alertSeverities = map[string]bool{"warning": true, "critical": true}

// List handles GET /v1/workspaces/{id}/alert-channels.
func (h *AlertHandler) List(w http.ResponseWriter, r *http.Request) {
	scope, ok := resolveWorkspaceScope(w, r, h.access, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	chans, err := h.store.Channels(r.Context(), scope[0])
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to list alert channels")
		return
	}
	items := make([]alertChannelView, len(chans))
	for i, c := range chans {
		items[i] = viewChannel(c)
	}
	respondJSON(w, http.StatusOK, map[string]any{"items": items, "email_enabled": h.dispatcher.EmailEnabled()})
}

type alertChannelRequest struct {
	Kind           string  `json:"kind"`
	Name           *string `json:"name"`
	Target         string  `json:"target"`
	MinSeverity    *string `json:"min_severity"`
	NotifyResolved *bool   `json:"notify_resolved"`
	Enabled        *bool   `json:"enabled"`
}

func decodeAlertChannel(w http.ResponseWriter, r *http.Request) (alertChannelRequest, bool) {
	var req alertChannelRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondDecodeError(w, err)
		return req, false
	}
	if req.Name != nil && (strings.TrimSpace(*req.Name) == "" || len(*req.Name) > 100) {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "name must be 1 to 100 characters")
		return req, false
	}
	if req.MinSeverity != nil && !alertSeverities[*req.MinSeverity] {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "min_severity must be warning or critical")
		return req, false
	}
	return req, true
}

// apply copies the fields the request set onto c.
func (req alertChannelRequest) apply(c *alert.Channel) {
	if req.Name != nil {
		c.Name = strings.TrimSpace(*req.Name)
	}
	if req.MinSeverity != nil {
		c.MinSeverity = *req.MinSeverity
	}
	if req.NotifyResolved != nil {
		c.NotifyResolved = *req.NotifyResolved
	}
	if req.Enabled != nil {
		c.Enabled = *req.Enabled
	}
}

// Create handles POST /v1/workspaces/{id}/alert-channels. A webhook's signing
// secret is in this response only.
func (h *AlertHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := requireOwner(w, r, h.owners)
	if !ok {
		return
	}
	req, ok := decodeAlertChannel(w, r)
	if !ok {
		return
	}
	if req.Name == nil {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "name is required")
		return
	}
	target, err := h.dispatcher.Validate(r.Context(), req.Kind, req.Target)
	if errors.Is(err, alert.ErrEmailDisabled) {
		respondError(w, http.StatusBadRequest, "EMAIL_DISABLED", "email delivery is not configured on this deployment")
		return
	}
	if err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_TARGET", err.Error())
		return
	}
	c := alert.Channel{WorkspaceID: workspaceID, Kind: req.Kind, Target: target, MinSeverity: "warning", NotifyResolved: true, Enabled: true, CreatedBy: userID}
	req.apply(&c)
	if c.Kind == alert.KindWebhook {
		b := make([]byte, 32)
		_, _ = rand.Read(b)
		c.Secret = "whsec_" + hex.EncodeToString(b)
	}
	if err := h.store.CreateChannel(r.Context(), &c); err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to create alert channel")
		return
	}
	v := viewChannel(c)
	v.Secret = c.Secret
	respondJSON(w, http.StatusCreated, v)
}

// channel loads the {channelId} channel of a workspace the caller owns, answering on failure.
func (h *AlertHandler) channel(w http.ResponseWriter, r *http.Request) (*alert.Channel, bool) {
	_, workspaceID, ok := requireOwner(w, r, h.owners)
	if !ok {
		return nil, false
	}
	c, err := h.store.Channel(r.Context(), workspaceID, chi.URLParam(r, "channelId"))
	if errors.Is(err, alert.ErrNotFound) {
		respondError(w, http.StatusNotFound, "ALERT_CHANNEL_NOT_FOUND", "alert channel not found")
		return nil, false
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to load alert channel")
		return nil, false
	}
	return c, true
}

// Update handles PATCH /v1/workspaces/{id}/alert-channels/{channelId}.
func (h *AlertHandler) Update(w http.ResponseWriter, r *http.Request) {
	c, ok := h.channel(w, r)
	if !ok {
		return
	}
	req, ok := decodeAlertChannel(w, r)
	if !ok {
		return
	}
	req.apply(c)
	if err := h.store.UpdateChannel(r.Context(), *c); err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to update alert channel")
		return
	}
	respondJSON(w, http.StatusOK, viewChannel(*c))
}

// Delete handles DELETE /v1/workspaces/{id}/alert-channels/{channelId}.
func (h *AlertHandler) Delete(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, ok := requireOwner(w, r, h.owners)
	if !ok {
		return
	}
	err := h.store.DeleteChannel(r.Context(), workspaceID, chi.URLParam(r, "channelId"))
	if errors.Is(err, alert.ErrNotFound) {
		respondError(w, http.StatusNotFound, "ALERT_CHANNEL_NOT_FOUND", "alert channel not found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to delete alert channel")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Test handles POST /v1/workspaces/{id}/alert-channels/{channelId}/test: it
// sends a sample message now and reports the outcome.
func (h *AlertHandler) Test(w http.ResponseWriter, r *http.Request) {
	c, ok := h.channel(w, r)
	if !ok {
		return
	}
	if err := h.dispatcher.Test(r.Context(), *c); err != nil {
		respondJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Deliveries handles GET /v1/incidents/{id}/deliveries.
func (h *AlertHandler) Deliveries(w http.ResponseWriter, r *http.Request) {
	scope, ok := resolveWorkspaceScope(w, r, h.access, "")
	if !ok {
		return
	}
	inc, err := h.incidents.Get(r.Context(), scope, chi.URLParam(r, "id"))
	if errors.Is(err, incident.ErrNotFound) {
		respondError(w, http.StatusNotFound, "INCIDENT_NOT_FOUND", "incident not found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to load incident")
		return
	}
	ds, err := h.store.Deliveries(r.Context(), inc.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to list deliveries")
		return
	}
	respondPage(w, ds, len(ds), 1, len(ds))
}
