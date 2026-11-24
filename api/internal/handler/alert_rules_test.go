package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/tracium/api/internal/alert"
	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/testing/mocks"
)

func ruleRouter(store *mocks.MockAlertStore) http.Handler {
	owners := &mocks.MockWorkspaceStore{Workspaces: []model.Workspace{{ID: "mine", UserID: "owner"}}}
	h := NewAlertHandler(store, store, alert.NewDispatcher(store, nil, alert.DefaultOptions()), incidentStore(), traceTestAccess{ids: []string{"mine"}}, owners)
	r := chi.NewRouter()
	r.Get("/v1/workspaces/{id}/alert-rules", h.ListRules)
	r.Post("/v1/workspaces/{id}/alert-rules", h.CreateRule)
	r.Patch("/v1/workspaces/{id}/alert-rules/{ruleId}", h.UpdateRule)
	r.Delete("/v1/workspaces/{id}/alert-rules/{ruleId}", h.DeleteRule)
	return r
}

func TestAlertRulesOwnerOnly(t *testing.T) {
	store := &mocks.MockAlertStore{}
	r := ruleRouter(store)
	body := `{"scope":"client","subject":"*","period":"day","threshold_usd":50}`
	for _, user := range []string{"member", "stranger"} {
		if rec := serveAs(r, user, http.MethodPost, "/v1/workspaces/mine/alert-rules", body); rec.Code != http.StatusNotFound {
			t.Fatalf("%s create: %d", user, rec.Code)
		}
		if rec := serveAs(r, user, http.MethodGet, "/v1/workspaces/mine/alert-rules", ""); rec.Code != http.StatusNotFound {
			t.Fatalf("%s list: %d", user, rec.Code)
		}
	}
	if len(store.RuleSet) != 0 {
		t.Fatal("non-owner created a rule")
	}
	rec := serveAs(r, "owner", http.MethodPost, "/v1/workspaces/mine/alert-rules", body)
	var created alert.Rule
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || rec.Code != http.StatusCreated || !created.Enabled || created.CreatedBy != "owner" || created.Subject != "*" {
		t.Fatalf("owner create: %d %s", rec.Code, rec.Body)
	}
	path := "/v1/workspaces/mine/alert-rules/" + created.ID
	if rec := serveAs(r, "member", http.MethodPatch, path, `{"enabled":false}`); rec.Code != http.StatusNotFound || !store.RuleSet[0].Enabled {
		t.Fatalf("member update: %d", rec.Code)
	}
	if rec := serveAs(r, "owner", http.MethodPatch, path, `{"enabled":false,"threshold_usd":75}`); rec.Code != http.StatusOK || store.RuleSet[0].Enabled || store.RuleSet[0].ThresholdUSD != 75 {
		t.Fatalf("owner update: %d %+v", rec.Code, store.RuleSet[0])
	}
	if rec := serveAs(r, "owner", http.MethodDelete, "/v1/workspaces/mine/alert-rules/nope", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("delete unknown: %d", rec.Code)
	}
	if rec := serveAs(r, "owner", http.MethodDelete, path, ""); rec.Code != http.StatusNoContent || len(store.RuleSet) != 0 {
		t.Fatalf("delete: %d", rec.Code)
	}
}

func TestAlertRuleValidation(t *testing.T) {
	store := &mocks.MockAlertStore{}
	r := ruleRouter(store)
	for _, body := range []string{
		`{"scope":"model","period":"day","threshold_usd":5}`,
		`{"scope":"workspace","period":"week","threshold_usd":5}`,
		`{"scope":"workspace","period":"day"}`,
		`{"scope":"workspace","period":"day","threshold_usd":0}`,
		`{"scope":"workspace","period":"day","threshold_usd":-1}`,
		`{"scope":"workflow","subject":"  ","period":"day","threshold_usd":5}`,
	} {
		if rec := serveAs(r, "owner", http.MethodPost, "/v1/workspaces/mine/alert-rules", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", body, rec.Code)
		}
	}
	rec := serveAs(r, "owner", http.MethodPost, "/v1/workspaces/mine/alert-rules", `{"scope":"workspace","subject":"ignored","period":"month","threshold_usd":500}`)
	if rec.Code != http.StatusCreated || store.RuleSet[0].Subject != "" {
		t.Fatalf("workspace rule: %d %+v", rec.Code, store.RuleSet)
	}
}
