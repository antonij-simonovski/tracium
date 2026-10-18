package handler

import (
	"context"
	"log"
	"net/http"
	"time"
)

// DependencyCheck is a named readiness probe for a backing dependency.
// Check must return nil when the dependency is reachable and healthy.
type DependencyCheck struct {
	Name  string
	Check func(ctx context.Context) error
}

// HealthHandler handles liveness and readiness probe endpoints.
// These routes are registered without auth middleware.
type HealthHandler struct {
	deps    []DependencyCheck
	timeout time.Duration
}

// NewHealthHandler constructs a HealthHandler. The supplied dependency checks
// are evaluated on every readiness probe; liveness never touches them.
func NewHealthHandler(deps ...DependencyCheck) *HealthHandler {
	return &HealthHandler{deps: deps, timeout: 3 * time.Second}
}

// Health handles GET /v1/health — liveness probe. It reports only that the
// process is running and able to serve HTTP; it deliberately does NOT check
// dependencies, so a transient ClickHouse/Postgres outage does not cause
// Kubernetes to restart an otherwise-healthy process.
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Ready handles GET /v1/ready — readiness probe. It returns 200 only when every
// registered dependency is reachable; otherwise it returns 503 with a per-check
// breakdown so an orchestrator stops routing traffic to this instance.
func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	checks := make(map[string]string, len(h.deps))
	ready := true
	for _, d := range h.deps {
		if err := d.Check(ctx); err != nil {
			ready = false
			log.Printf("ready: %s: %v", d.Name, err)
			checks[d.Name] = "unavailable"
			continue
		}
		checks[d.Name] = "ok"
	}

	status := "ok"
	code := http.StatusOK
	if !ready {
		status = "unavailable"
		code = http.StatusServiceUnavailable
	}

	respondJSON(w, code, map[string]any{
		"status": status,
		"checks": checks,
	})
}
