package handler

import (
	"errors"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"github.com/tracium/api/internal/incident"
	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/internal/query"
	"github.com/tracium/api/internal/review"
)

var queueReasons = map[string]bool{"incident": true, "truncated": true, "refused": true, "tool_error": true, "sample": true}

const (
	queueFlaggedWindow = 7 * 24 * time.Hour
	queueSampleWindow  = 24 * time.Hour
	// Every queueSampleEvery-th item is a random unflagged run, so accuracy is not measured only on suspicious runs.
	queueSampleEvery = 5
	// shortcut: only the newest queueScan flagged runs are read, so once they are all reviewed older ones are not offered; page past reviewed runs if queues run dry.
	queueScan = 200
)

// ReviewQueueHandler lists unreviewed runs worth a reviewer's time.
type ReviewQueueHandler struct {
	traces    query.TraceRepository
	reviews   review.Store
	incidents incident.Store
	access    WorkspaceAccess
}

func NewReviewQueueHandler(traces query.TraceRepository, reviews review.Store, incidents incident.Store, access WorkspaceAccess) *ReviewQueueHandler {
	return &ReviewQueueHandler{traces: traces, reviews: reviews, incidents: incidents, access: access}
}

// Queue handles GET /v1/reviews/queue?workspace_id=&workflow=&limit=: example
// runs of open incidents, then runs flagged as silent failures in the last 7
// days, with a random sample of unflagged runs from the last 24 hours mixed in.
func (h *ReviewQueueHandler) Queue(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	scope, ok := resolveWorkspaceScope(w, r, h.access, q.Get("workspace_id"))
	if !ok {
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
	workflow, now, ctx := q.Get("workflow"), time.Now(), r.Context()
	fail := func() {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to build the review queue")
	}

	open, _, err := h.incidents.List(ctx, scope, incident.StatusOpen, 1, 100)
	if err != nil {
		fail()
		return
	}
	type example struct{ traceID, workspaceID, incidentID string }
	var examples []example
	seen := map[string]bool{}
	for _, inc := range open {
		if workflow != "" && inc.Scope == "workflow" && inc.Subject != workflow {
			continue
		}
		for _, s := range inc.Signals {
			for _, id := range s.TraceIDs {
				if !seen[id] {
					seen[id] = true
					examples = append(examples, example{id, inc.WorkspaceID, inc.ID})
				}
			}
		}
	}
	flagged, err := h.list(r, query.TraceFilter{WorkspaceIDs: scope, Workflow: workflow, SilentFailure: "any", StartAfter: now.Add(-queueFlaggedWindow)}, false)
	if err != nil {
		fail()
		return
	}
	recent, err := h.list(r, query.TraceFilter{WorkspaceIDs: scope, Workflow: workflow, StartAfter: now.Add(-queueSampleWindow)}, true)
	if err != nil {
		fail()
		return
	}
	ids := make([]string, 0, len(examples)+len(flagged)+len(recent))
	for _, e := range examples {
		ids = append(ids, e.traceID)
	}
	for _, t := range append(flagged, recent...) {
		ids = append(ids, t.TraceID)
	}
	reviewed, err := h.reviews.Reviewed(ctx, scope, ids)
	if err != nil {
		fail()
		return
	}

	var primary []model.QueueItem
	for _, e := range examples {
		if reviewed[e.traceID] || len(primary) >= limit {
			continue
		}
		t, err := h.traces.GetTrace(ctx, e.traceID, []string{e.workspaceID})
		if errors.Is(err, query.ErrNotFound) {
			continue
		}
		if err != nil {
			fail()
			return
		}
		if workflow == "" || t.Name == workflow {
			primary = append(primary, model.QueueItem{Trace: *t, Reason: "incident", IncidentID: e.incidentID})
		}
	}
	for _, t := range flagged {
		if !seen[t.TraceID] && !reviewed[t.TraceID] && len(t.SilentFailures) > 0 {
			seen[t.TraceID] = true
			primary = append(primary, model.QueueItem{Trace: t, Reason: t.SilentFailures[0]})
		}
	}
	var samples []model.QueueItem
	for _, i := range rand.Perm(len(recent)) {
		if t := recent[i]; !seen[t.TraceID] && !reviewed[t.TraceID] && len(t.SilentFailures) == 0 {
			samples = append(samples, model.QueueItem{Trace: t, Reason: "sample"})
		}
	}
	respondJSON(w, http.StatusOK, buildQueue(primary, samples, limit))
}

// list reads one page of runs; random picks a random page, so a busy
// workspace's sample is not only its newest runs.
func (h *ReviewQueueHandler) list(r *http.Request, f query.TraceFilter, random bool) ([]model.Trace, error) {
	f.Page, f.PageSize = 1, queueScan
	if err := f.Validate(); err != nil {
		return nil, err
	}
	traces, total, err := h.traces.ListTraces(r.Context(), f)
	if err != nil || !random || total <= queueScan {
		return traces, err
	}
	f.Page = 1 + rand.IntN(int((total+queueScan-1)/queueScan))
	traces, _, err = h.traces.ListTraces(r.Context(), f)
	return traces, err
}

// buildQueue keeps primary's order and puts a sample at every
// queueSampleEvery-th place; when either list runs out the other fills the rest.
func buildQueue(primary, samples []model.QueueItem, limit int) []model.QueueItem {
	out := []model.QueueItem{}
	for len(out) < limit && (len(primary) > 0 || len(samples) > 0) {
		if len(samples) > 0 && (len(primary) == 0 || len(out)%queueSampleEvery == queueSampleEvery-1) {
			out, samples = append(out, samples[0]), samples[1:]
		} else {
			out, primary = append(out, primary[0]), primary[1:]
		}
	}
	return out
}
