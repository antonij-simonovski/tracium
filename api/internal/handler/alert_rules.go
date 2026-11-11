package handler

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/tracium/api/internal/alert"
)

var (
	ruleScopes  = map[string]bool{"workspace": true, "workflow": true, "client": true}
	rulePeriods = map[string]bool{"day": true, "month": true}
)

type alertRuleRequest struct {
	Scope        string   `json:"scope"`
	Subject      string   `json:"subject"`
	Period       string   `json:"period"`
	ThresholdUSD *float64 `json:"threshold_usd"`
	Enabled      *bool    `json:"enabled"`
}

func decodeAlertRule(w http.ResponseWriter, r *http.Request) (alertRuleRequest, bool) {
	var req alertRuleRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondDecodeError(w, err)
		return req, false
	}
	if t := req.ThresholdUSD; t != nil && (!(*t > 0) || math.IsInf(*t, 0)) {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "threshold_usd must be a positive number")
		return req, false
	}
	return req, true
}

// ListRules handles GET /v1/workspaces/{id}/alert-rules.
func (h *AlertHandler) ListRules(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, ok := requireOwner(w, r, h.owners)
	if !ok {
		return
	}
	rules, err := h.rules.Rules(r.Context(), workspaceID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to list alert rules")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"items": rules})
}

// CreateRule handles POST /v1/workspaces/{id}/alert-rules.
func (h *AlertHandler) CreateRule(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := requireOwner(w, r, h.owners)
	if !ok {
		return
	}
	req, ok := decodeAlertRule(w, r)
	if !ok {
		return
	}
	subject := strings.TrimSpace(req.Subject)
	if req.Scope == "workspace" {
		subject = ""
	}
	switch {
	case !ruleScopes[req.Scope]:
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "scope must be workspace, workflow or client")
		return
	case !rulePeriods[req.Period]:
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "period must be day or month")
		return
	case req.ThresholdUSD == nil:
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "threshold_usd is required")
		return
	case req.Scope != "workspace" && (subject == "" || len(subject) > 200):
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "subject must be 1 to 200 characters, or * for each one")
		return
	}
	rule := alert.Rule{WorkspaceID: workspaceID, Scope: req.Scope, Subject: subject, Period: req.Period,
		ThresholdUSD: *req.ThresholdUSD, Enabled: req.Enabled == nil || *req.Enabled, CreatedBy: userID}
	if err := h.rules.CreateRule(r.Context(), &rule); err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to create alert rule")
		return
	}
	respondJSON(w, http.StatusCreated, rule)
}

// UpdateRule handles PATCH /v1/workspaces/{id}/alert-rules/{ruleId}.
func (h *AlertHandler) UpdateRule(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, ok := requireOwner(w, r, h.owners)
	if !ok {
		return
	}
	rule, err := h.rules.Rule(r.Context(), workspaceID, chi.URLParam(r, "ruleId"))
	if !ruleFound(w, err) {
		return
	}
	req, ok := decodeAlertRule(w, r)
	if !ok {
		return
	}
	if req.ThresholdUSD != nil {
		rule.ThresholdUSD = *req.ThresholdUSD
	}
	if req.Enabled != nil {
		rule.Enabled = *req.Enabled
	}
	if !ruleFound(w, h.rules.UpdateRule(r.Context(), *rule)) {
		return
	}
	respondJSON(w, http.StatusOK, rule)
}

// DeleteRule handles DELETE /v1/workspaces/{id}/alert-rules/{ruleId}.
func (h *AlertHandler) DeleteRule(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, ok := requireOwner(w, r, h.owners)
	if !ok {
		return
	}
	if !ruleFound(w, h.rules.DeleteRule(r.Context(), workspaceID, chi.URLParam(r, "ruleId"))) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func ruleFound(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, alert.ErrRuleNotFound):
		respondError(w, http.StatusNotFound, "ALERT_RULE_NOT_FOUND", "alert rule not found")
	case err != nil:
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to load alert rule")
	default:
		return true
	}
	return false
}
