package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/tracium/api/internal/alert"
	"github.com/tracium/api/internal/middleware"
	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/testing/mocks"
)

func alertRouter(store *mocks.MockAlertStore) http.Handler {
	opts := alert.DefaultOptions()
	opts.AllowPrivateURLs = true
	owners := &mocks.MockWorkspaceStore{Workspaces: []model.Workspace{{ID: "mine", UserID: "owner"}}}
	h := NewAlertHandler(store, store, alert.NewDispatcher(store, nil, opts), incidentStore(), traceTestAccess{ids: []string{"mine"}}, owners)
	r := chi.NewRouter()
	r.Get("/v1/workspaces/{id}/alert-channels", h.List)
	r.Post("/v1/workspaces/{id}/alert-channels", h.Create)
	r.Patch("/v1/workspaces/{id}/alert-channels/{channelId}", h.Update)
	r.Delete("/v1/workspaces/{id}/alert-channels/{channelId}", h.Delete)
	r.Get("/v1/incidents/{id}/deliveries", h.Deliveries)
	return r
}

func serveAs(r http.Handler, user, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req = req.WithContext(middleware.ContextWithPrincipal(req.Context(), &model.Principal{UserID: user}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestAlertChannelsOwnerOnly(t *testing.T) {
	store := &mocks.MockAlertStore{}
	r := alertRouter(store)
	body := `{"kind":"webhook","name":"hook","target":"http://10.0.0.1/hook"}`
	if rec := serveAs(r, "member", http.MethodPost, "/v1/workspaces/mine/alert-channels", body); rec.Code != http.StatusNotFound || len(store.ChannelSet) != 0 {
		t.Fatalf("member create: %d", rec.Code)
	}
	rec := serveAs(r, "owner", http.MethodPost, "/v1/workspaces/mine/alert-channels", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("owner create: %d %s", rec.Code, rec.Body)
	}
	var created struct{ ID, Target, Secret string }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if !strings.HasPrefix(created.Secret, "whsec_") || created.Target != "http://10.0.0.1/…/…hook" {
		t.Fatalf("created %+v", created)
	}
	if rec := serveAs(r, "member", http.MethodPatch, "/v1/workspaces/mine/alert-channels/"+created.ID, `{"enabled":false}`); rec.Code != http.StatusNotFound {
		t.Fatalf("member update: %d", rec.Code)
	}
	if rec := serveAs(r, "owner", http.MethodPatch, "/v1/workspaces/mine/alert-channels/"+created.ID, `{"enabled":false,"min_severity":"critical"}`); rec.Code != http.StatusOK || store.ChannelSet[0].Enabled || store.ChannelSet[0].MinSeverity != "critical" {
		t.Fatalf("owner update: %d %+v", rec.Code, store.ChannelSet[0])
	}

	list := serveAs(r, "member", http.MethodGet, "/v1/workspaces/mine/alert-channels", "").Body.String()
	if strings.Contains(list, "whsec_") || strings.Contains(list, "10.0.0.1/hook") || !strings.Contains(list, `"email_enabled":false`) {
		t.Fatalf("list leaks the target or secret: %s", list)
	}
	if rec := serveAs(r, "stranger", http.MethodGet, "/v1/workspaces/theirs/alert-channels", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("non-member list: %d", rec.Code)
	}
	if rec := serveAs(r, "owner", http.MethodDelete, "/v1/workspaces/mine/alert-channels/"+created.ID, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}
}

func TestAlertChannelValidation(t *testing.T) {
	r := alertRouter(&mocks.MockAlertStore{})
	for body, code := range map[string]string{
		`{"kind":"email","name":"mail","target":"a@example.com"}`:        "EMAIL_DISABLED",
		`{"kind":"webhook","name":"hook","target":"not a url"}`:          "INVALID_TARGET",
		`{"kind":"webhook","target":"https://example.com"}`:              "BAD_REQUEST",
		`{"kind":"slack","name":"s","target":"x","min_severity":"info"}`: "BAD_REQUEST",
	} {
		rec := serveAs(r, "owner", http.MethodPost, "/v1/workspaces/mine/alert-channels", body)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), code) {
			t.Errorf("%s: %d %s", body, rec.Code, rec.Body)
		}
	}
}

func TestDeliveriesAcrossWorkspacesAre404(t *testing.T) {
	r := alertRouter(&mocks.MockAlertStore{})
	if rec := serveAs(r, "u1", http.MethodGet, "/v1/incidents/other/deliveries", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("other workspace: %d", rec.Code)
	}
	if rec := serveAs(r, "u1", http.MethodGet, "/v1/incidents/own/deliveries", ""); rec.Code != http.StatusOK {
		t.Fatalf("own: %d", rec.Code)
	}
}
