package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/tracium/api/internal/incident"
	"github.com/tracium/api/internal/middleware"
	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/testing/mocks"
)

type recordingNotifier struct{ events []incident.Event }

func (n *recordingNotifier) Notify(_ context.Context, e incident.Event) error {
	n.events = append(n.events, e)
	return nil
}

func incidentRouter(store *mocks.MockIncidentStore, notifier incident.Notifier) http.Handler {
	h := NewIncidentHandler(store, traceTestAccess{ids: []string{"mine"}}, notifier)
	r := chi.NewRouter()
	r.Get("/v1/incidents", h.List)
	r.Get("/v1/incidents/{id}", h.Get)
	r.Post("/v1/incidents/{id}/resolve", h.Resolve)
	r.Post("/v1/incidents/{id}/mute", h.Mute)
	r.Get("/v1/workspaces/{id}/incident-mutes", h.ListMutes)
	r.Delete("/v1/workspaces/{id}/incident-mutes/{muteId}", h.DeleteMute)
	return r
}

func serveIncident(r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req = req.WithContext(middleware.ContextWithPrincipal(req.Context(), &model.Principal{UserID: "u1"}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func incidentStore() *mocks.MockIncidentStore {
	return &mocks.MockIncidentStore{Incidents: []incident.Incident{
		{ID: "own", WorkspaceID: "mine", Scope: "workflow", Subject: "wf", Status: incident.StatusOpen},
		{ID: "other", WorkspaceID: "theirs", Scope: "workflow", Subject: "wf", Status: incident.StatusOpen},
	}}
}

func TestIncidentsAcrossWorkspacesAre404(t *testing.T) {
	for _, metric := range []string{"release_regression", "release_regression:truncation_rate", "release_regression:cost_per_run"} {
		if rec := serveIncident(incidentRouter(incidentStore(), nil), http.MethodPost, "/v1/incidents/own/mute", `{"duration":"24h","metric":"`+metric+`"}`); rec.Code != http.StatusCreated {
			t.Errorf("%s: got %d, want 201", metric, rec.Code)
		}
	}
	store := incidentStore()
	notifier := &recordingNotifier{}
	r := incidentRouter(store, notifier)
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/v1/incidents/other", ""},
		{http.MethodPost, "/v1/incidents/other/resolve", ""},
		{http.MethodPost, "/v1/incidents/other/mute", `{"duration":"24h"}`},
	} {
		if rec := serveIncident(r, tc.method, tc.path, tc.body); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: got %d, want 404", tc.method, tc.path, rec.Code)
		}
	}
	if store.Incidents[1].Status != incident.StatusOpen || len(store.MuteList) != 0 || len(notifier.events) != 0 {
		t.Fatal("another workspace's incident was changed")
	}
	if rec := serveIncident(r, http.MethodGet, "/v1/workspaces/theirs/incident-mutes", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("mutes of another workspace: got %d", rec.Code)
	}
	if rec := serveIncident(r, http.MethodGet, "/v1/incidents", ""); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"other"`) {
		t.Fatalf("list leaked another workspace: %d %s", rec.Code, rec.Body)
	}
}

func TestIncidentResolveNotifies(t *testing.T) {
	store := incidentStore()
	notifier := &recordingNotifier{}
	r := incidentRouter(store, notifier)
	if rec := serveIncident(r, http.MethodPost, "/v1/incidents/own/resolve", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"resolved_by":"u1"`) {
		t.Fatalf("resolve: %d %s", rec.Code, rec.Body)
	}
	if rec := serveIncident(r, http.MethodPost, "/v1/incidents/own/resolve", ""); rec.Code != http.StatusOK {
		t.Fatalf("second resolve: %d", rec.Code)
	}
	if len(notifier.events) != 1 || notifier.events[0].Type != incident.EventResolved {
		t.Fatalf("want one resolved event, got %+v", notifier.events)
	}
}

func TestIncidentMute(t *testing.T) {
	for _, body := range []string{`{"duration":"1h"}`, `{}`, `{"duration":"24h","metric":"nope"}`, `not json`,
		`{"duration":"24h","metric":"release_regression:nope"}`, `{"duration":"24h","metric":"release_regression:"}`} {
		if rec := serveIncident(incidentRouter(incidentStore(), nil), http.MethodPost, "/v1/incidents/own/mute", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", body, rec.Code)
		}
	}
	store := incidentStore()
	notifier := &recordingNotifier{}
	r := incidentRouter(store, notifier)
	rec := serveIncident(r, http.MethodPost, "/v1/incidents/own/mute", `{"duration":"forever","metric":"truncation_rate"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("mute: %d %s", rec.Code, rec.Body)
	}
	m := store.MuteList[0]
	if m.WorkspaceID != "mine" || m.Subject != "wf" || m.Metric != "truncation_rate" || m.Until != nil || m.CreatedBy != "u1" {
		t.Fatalf("unexpected mute %+v", m)
	}
	if store.Incidents[0].Status != incident.StatusResolved || len(notifier.events) != 0 {
		t.Fatal("mute should resolve the incident without notifying")
	}
	serveIncident(r, http.MethodPost, "/v1/incidents/own/mute", `{"duration":"7d"}`)
	if store.MuteList[1].Until == nil {
		t.Fatal("7d mute has no end")
	}
	if rec := serveIncident(r, http.MethodDelete, "/v1/workspaces/mine/incident-mutes/"+m.ID, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete mute: %d", rec.Code)
	}
	if rec := serveIncident(r, http.MethodDelete, "/v1/workspaces/mine/incident-mutes/"+m.ID, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing mute: %d", rec.Code)
	}
}

func TestIncidentListValidation(t *testing.T) {
	r := incidentRouter(incidentStore(), nil)
	for _, q := range []string{"status=closed", "page=0", "page_size=101"} {
		if rec := serveIncident(r, http.MethodGet, "/v1/incidents?"+q, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", q, rec.Code)
		}
	}
}

func TestIncidentListReportsLastEvaluation(t *testing.T) {
	store := incidentStore()
	r := incidentRouter(store, nil)
	if rec := serveIncident(r, http.MethodGet, "/v1/incidents", ""); !strings.Contains(rec.Body.String(), `"last_evaluated_at":null`) {
		t.Fatalf("never evaluated: %s", rec.Body.String())
	}
	store.Evaluated = map[string]time.Time{"mine": time.Date(2026, 11, 10, 20, 0, 0, 0, time.UTC), "theirs": time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	if rec := serveIncident(r, http.MethodGet, "/v1/incidents", ""); !strings.Contains(rec.Body.String(), `"last_evaluated_at":"2026-11-10T20:00:00Z"`) {
		t.Fatalf("last evaluated: %s", rec.Body.String())
	}
}
