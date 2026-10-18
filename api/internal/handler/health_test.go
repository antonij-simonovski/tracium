package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func doReady(h *HealthHandler) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/v1/ready", nil)
	rr := httptest.NewRecorder()
	h.Ready(rr, req)
	return rr
}

func TestReadyAllHealthy(t *testing.T) {
	h := NewHealthHandler(
		DependencyCheck{Name: "clickhouse", Check: func(context.Context) error { return nil }},
		DependencyCheck{Name: "postgres", Check: func(context.Context) error { return nil }},
	)
	rr := doReady(h)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
}

func TestReadyDependencyDown(t *testing.T) {
	h := NewHealthHandler(
		DependencyCheck{Name: "clickhouse", Check: func(context.Context) error { return errors.New("connection refused") }},
		DependencyCheck{Name: "postgres", Check: func(context.Context) error { return nil }},
	)
	rr := doReady(h)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rr.Code)
	}

	var body struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "unavailable" {
		t.Errorf("status = %q, want unavailable", body.Status)
	}
	if body.Checks["postgres"] != "ok" {
		t.Errorf("postgres check = %q, want ok", body.Checks["postgres"])
	}
	if body.Checks["clickhouse"] != "unavailable" {
		t.Errorf("clickhouse check = %q, want unavailable", body.Checks["clickhouse"])
	}
}

func TestLivenessIgnoresDependencies(t *testing.T) {
	h := NewHealthHandler(
		DependencyCheck{Name: "clickhouse", Check: func(context.Context) error { return errors.New("down") }},
	)
	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	rr := httptest.NewRecorder()
	h.Health(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("liveness status = %d, want 200 regardless of dependencies", rr.Code)
	}
}
