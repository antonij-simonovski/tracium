package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/tracium/api/internal/incident"
)

type Backtests interface {
	Backtest(ctx context.Context, workspaceID, rng string) (*incident.BacktestResult, error)
}

// BacktestHandler serves what the incident evaluator would have opened over a past range.
type BacktestHandler struct {
	backtests Backtests
	access    WorkspaceAccess
}

func NewBacktestHandler(b Backtests, access WorkspaceAccess) *BacktestHandler {
	return &BacktestHandler{backtests: b, access: access}
}

// Get handles GET /v1/incidents/backtest?workspace_id=&range=7d|30d.
func (h *BacktestHandler) Get(w http.ResponseWriter, r *http.Request) {
	// A replay may run for a minute, longer than the server's write timeout.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(70 * time.Second))
	q := r.URL.Query()
	rng := q.Get("range")
	if rng == "" {
		rng = "30d"
	}
	if rng != "7d" && rng != "30d" {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "range must be 7d or 30d")
		return
	}
	if q.Get("workspace_id") == "" {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "workspace_id is required")
		return
	}
	scope, ok := resolveWorkspaceScope(w, r, h.access, q.Get("workspace_id"))
	if !ok {
		return
	}
	res, err := h.backtests.Backtest(r.Context(), scope[0], rng)
	if errors.Is(err, context.DeadlineExceeded) {
		respondError(w, http.StatusGatewayTimeout, "BACKTEST_TIMEOUT", "the backtest took too long; try range=7d")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to run the backtest")
		return
	}
	respondJSON(w, http.StatusOK, res)
}
