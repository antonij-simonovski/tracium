package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/tracium/api/internal/middleware"
	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/internal/query"
	"github.com/tracium/api/internal/review"
)

const maxReviewBody = 1 << 20

// keyLookupWindow is how far back PutByKey looks for a run: the default retention.
const keyLookupWindow = 90 * 24 * time.Hour

// ReviewHandler serves reviews of runs and spans and the accuracy built from them.
// Routes with a {spanId} param act on that span's review, the rest on the run's.
type ReviewHandler struct {
	traces query.TraceRepository
	store  review.Store
	access WorkspaceAccess
}

func NewReviewHandler(traces query.TraceRepository, store review.Store, access WorkspaceAccess) *ReviewHandler {
	return &ReviewHandler{traces: traces, store: store, access: access}
}

type reviewRequest struct {
	Verdict string `json:"verdict"`
	// Output replaces the traced output, for apps that post-process it after the trace.
	Output          *string `json:"output"`
	CorrectedOutput *string `json:"corrected_output"`
	// Corrections sets only these dotted field paths on the output; the rest grade as correct.
	Corrections map[string]json.RawMessage `json:"corrections"`
	Note        string                     `json:"note"`
	// Reason is the review queue's reason when the run was reviewed from it.
	Reason string `json:"reason"`
}

// decodeReview reads and checks a review body; it answers the request itself on failure.
func decodeReview(w http.ResponseWriter, r *http.Request) (*model.Principal, reviewRequest, bool) {
	var req reviewRequest
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok || principal.UserID == "" {
		respondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "user identity could not be resolved")
		return principal, req, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxReviewBody)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondDecodeError(w, err)
		return principal, req, false
	}
	if req.CorrectedOutput != nil && req.Corrections != nil {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "send corrected_output or corrections, not both")
		return principal, req, false
	}
	if req.Reason != "" && !queueReasons[req.Reason] {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "reason must be incident, truncated, refused, tool_error or sample")
		return principal, req, false
	}
	if req.CorrectedOutput == nil && req.Corrections == nil && !validVerdict(req.Verdict) {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "verdict must be correct, incorrect or missing when there is no correction")
		return principal, req, false
	}
	return principal, req, true
}

func validVerdict(v string) bool {
	return v == model.VerdictCorrect || v == model.VerdictIncorrect || v == model.VerdictMissing
}

// Put handles PUT /v1/traces/{id}/review and /v1/traces/{id}/spans/{spanId}/review.
// With a correction the verdict is derived (per field for JSON outputs, and
// missing when there was no output); without one the verdict is required. Ingest keys may call it, so evaluators
// can send reviews; a trace still being ingested answers 404.
func (h *ReviewHandler) Put(w http.ResponseWriter, r *http.Request) {
	principal, req, ok := decodeReview(w, r)
	if !ok {
		return
	}
	scope, ok := resolveWorkspaceScope(w, r, h.access, r.URL.Query().Get("workspace_id"))
	if !ok {
		return
	}
	h.save(w, r, principal, req, scope, chi.URLParam(r, "id"), chi.URLParam(r, "spanId"), "")
}

// PutByKey handles PUT /v1/workflows/{name}/reviews?key=&value=[&span=]: it reviews
// the newest run of the workflow in the last 90 days with a span whose custom
// attribute key equals value, so callers need not keep Tracium's trace id.
// span names a span of that run to review instead of the whole run.
func (h *ReviewHandler) PutByKey(w http.ResponseWriter, r *http.Request) {
	principal, req, ok := decodeReview(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	key, value := q.Get("key"), q.Get("value")
	if key == "" || value == "" {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", "key and value are required")
		return
	}
	scope, ok := resolveWorkspaceScope(w, r, h.access, q.Get("workspace_id"))
	if !ok {
		return
	}
	f := query.TraceFilter{WorkspaceIDs: scope, Workflow: chi.URLParam(r, "name"), AttrKey: key, AttrValue: value, Page: 1, PageSize: 1,
		StartAfter: time.Now().Add(-keyLookupWindow)}
	if err := f.Validate(); err != nil {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	runs, _, err := h.traces.ListTraces(r.Context(), f)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	if len(runs) == 0 {
		respondError(w, http.StatusNotFound, "TRACE_NOT_FOUND", "no run of this workflow in the last 90 days has "+key+" = "+value)
		return
	}
	h.save(w, r, principal, req, scope, runs[0].TraceID, "", q.Get("span"))
}

// save reviews the run traceID, or one span of it: spanID picks the span by id,
// spanName by name (the latest one, when the run has several).
func (h *ReviewHandler) save(w http.ResponseWriter, r *http.Request, principal *model.Principal, req reviewRequest, scope []string, traceID, spanID, spanName string) {
	trace, err := h.traces.GetTrace(r.Context(), traceID, scope)
	if errors.Is(err, query.ErrNotFound) {
		respondError(w, http.StatusNotFound, "TRACE_NOT_FOUND", "trace not found")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	spans, err := h.traces.GetSpans(r.Context(), traceID, []string{trace.WorkspaceID})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	rev := &model.Review{
		TraceID:         traceID,
		WorkspaceID:     trace.WorkspaceID,
		WorkflowName:    trace.Name,
		WorkflowVersion: trace.WorkflowVersion,
		Verdict:         req.Verdict,
		Output:          review.RunOutput(spans),
		CorrectedOutput: req.CorrectedOutput,
		Note:            req.Note,
		ReviewedBy:      principal.UserID,
		Source:          model.ReviewSourceUser,
		Fields:          []model.ReviewField{},
		Reason:          req.Reason,
	}
	for _, sp := range spans {
		if rev.Reason == "" && sp.SilentFailure != "" {
			rev.Reason = sp.SilentFailure
		}
	}
	if principal.WorkspaceID != "" {
		rev.Source = model.ReviewSourceAPI
	}
	if spanID != "" || spanName != "" {
		i := -1
		for j, s := range spans {
			match := s.SpanID == spanID
			if spanID == "" {
				match = s.Name == spanName && (i < 0 || s.StartTimeMs >= spans[i].StartTimeMs)
			}
			if match {
				i = j
			}
		}
		if i < 0 {
			respondError(w, http.StatusNotFound, "SPAN_NOT_FOUND", "span not found")
			return
		}
		rev.SpanID, rev.SpanName, rev.Output = spans[i].SpanID, spans[i].Name, spans[i].Output
	}
	if req.Output != nil {
		rev.Output = *req.Output
	}
	if req.Corrections != nil {
		patched, err := review.Patch(rev.Output, req.Corrections)
		if err != nil {
			respondError(w, http.StatusBadRequest, "BAD_REQUEST", "corrections: "+err.Error())
			return
		}
		rev.CorrectedOutput = &patched
	}
	if c := rev.CorrectedOutput; c != nil && strings.TrimSpace(rev.Output) == "" {
		rev.Verdict = model.VerdictMissing
	} else if c != nil {
		if fields, graded := review.Grade(rev.Output, *c); graded {
			rev.Fields, rev.Verdict = fields, review.Verdict(fields)
		} else if _, isObject := review.Grade(rev.Output, rev.Output); isObject {
			// Otherwise a typo in the JSON would count as a wrong run with no wrong field.
			respondError(w, http.StatusBadRequest, "BAD_REQUEST", "corrected_output must be a JSON object, like the output")
			return
		} else if strings.TrimSpace(*c) == strings.TrimSpace(rev.Output) {
			rev.Verdict = model.VerdictCorrect
		} else {
			rev.Verdict = model.VerdictIncorrect
		}
	} else if fields, graded := review.Grade(rev.Output, rev.Output); graded {
		// Without the wrong fields, a rejected JSON output would leave field accuracy untouched.
		if rev.Verdict == model.VerdictIncorrect {
			respondError(w, http.StatusBadRequest, "BAD_REQUEST", "send corrections or corrected_output to mark a JSON output incorrect")
			return
		}
		// An approved JSON output counts toward every field's accuracy.
		if rev.Verdict == model.VerdictCorrect {
			rev.Fields = fields
		}
	}
	err = h.store.Upsert(r.Context(), rev)
	if errors.Is(err, review.ErrUserReview) {
		respondError(w, http.StatusConflict, "REVIEWED_BY_USER", err.Error())
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to save review")
		return
	}
	// Ingest keys never read telemetry back; even the derived verdict would let them test guesses of the output.
	if rev.Source == model.ReviewSourceAPI {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	respondJSON(w, http.StatusOK, rev)
}

// Get handles GET /v1/traces/{id}/review and the span route.
func (h *ReviewHandler) Get(w http.ResponseWriter, r *http.Request) {
	scope, ok := resolveWorkspaceScope(w, r, h.access, r.URL.Query().Get("workspace_id"))
	if !ok {
		return
	}
	rev, err := h.store.Get(r.Context(), chi.URLParam(r, "id"), chi.URLParam(r, "spanId"), scope)
	if errors.Is(err, review.ErrNotFound) {
		respondError(w, http.StatusNotFound, "REVIEW_NOT_FOUND", "no review")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to load review")
		return
	}
	respondJSON(w, http.StatusOK, rev)
}

// Delete handles DELETE /v1/traces/{id}/review and the span route.
func (h *ReviewHandler) Delete(w http.ResponseWriter, r *http.Request) {
	scope, ok := resolveWorkspaceScope(w, r, h.access, r.URL.Query().Get("workspace_id"))
	if !ok {
		return
	}
	err := h.store.Delete(r.Context(), chi.URLParam(r, "id"), chi.URLParam(r, "spanId"), scope)
	if errors.Is(err, review.ErrNotFound) {
		respondError(w, http.StatusNotFound, "REVIEW_NOT_FOUND", "no review")
		return
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to delete review")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// List handles GET /v1/workflows/{name}/reviews?version=&verdict=&field=&span=.
// verdict takes a comma-separated list, e.g. incorrect,missing.
// span picks reviews of spans with that name instead of run reviews; so do
// Accuracy and Fields.
func (h *ReviewHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	scope, ok := resolveWorkspaceScope(w, r, h.access, q.Get("workspace_id"))
	if !ok {
		return
	}
	f := review.ListFilter{
		WorkspaceIDs: scope,
		Workflow:     chi.URLParam(r, "name"),
		Span:         q.Get("span"),
		Version:      q.Get("version"),
		WrongField:   q.Get("field"),
		Page:         1,
		PageSize:     20,
	}
	if v := q.Get("verdict"); v != "" {
		f.Verdicts = strings.Split(v, ",")
		for _, v := range f.Verdicts {
			if !validVerdict(v) {
				respondError(w, http.StatusBadRequest, "BAD_REQUEST", "verdict must be correct, incorrect or missing")
				return
			}
		}
	}
	for key, dst := range map[string]*int{"page": &f.Page, "page_size": &f.PageSize} {
		if s := q.Get(key); s != "" {
			n, err := strconv.Atoi(s)
			if err != nil || n < 1 || (key == "page_size" && n > 100) {
				respondError(w, http.StatusBadRequest, "BAD_REQUEST", key+" is out of range")
				return
			}
			*dst = n
		}
	}
	reviews, total, err := h.store.List(r.Context(), f)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to list reviews")
		return
	}
	respondPage(w, reviews, total, f.Page, f.PageSize)
}

// Accuracy handles GET /v1/workflows/{name}/accuracy[?span=|?all_spans=true]: run
// accuracy per version, all time; all_spans returns the run and every span at once.
func (h *ReviewHandler) Accuracy(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	scope, ok := resolveWorkspaceScope(w, r, h.access, q.Get("workspace_id"))
	if !ok {
		return
	}
	span := q.Get("span")
	sp := &span
	if q.Get("all_spans") == "true" {
		sp = nil
	}
	acc, err := h.store.Accuracy(r.Context(), scope, chi.URLParam(r, "name"), sp)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to load accuracy")
		return
	}
	respondJSON(w, http.StatusOK, acc)
}

// Spans handles GET /v1/workflows/{name}/review-spans[?version=]: span names with
// reviews; without version, across all versions.
func (h *ReviewHandler) Spans(w http.ResponseWriter, r *http.Request) {
	scope, ok := resolveWorkspaceScope(w, r, h.access, r.URL.Query().Get("workspace_id"))
	if !ok {
		return
	}
	var version *string
	if q := r.URL.Query(); q.Has("version") {
		v := q.Get("version")
		version = &v
	}
	spans, err := h.store.Spans(r.Context(), scope, chi.URLParam(r, "name"), version)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to load reviewed spans")
		return
	}
	respondJSON(w, http.StatusOK, spans)
}

// Fields handles GET /v1/workflows/{name}/fields?version=: field accuracy, worst first.
func (h *ReviewHandler) Fields(w http.ResponseWriter, r *http.Request) {
	scope, ok := resolveWorkspaceScope(w, r, h.access, r.URL.Query().Get("workspace_id"))
	if !ok {
		return
	}
	acc, err := h.store.FieldAccuracy(r.Context(), scope, chi.URLParam(r, "name"), r.URL.Query().Get("span"), r.URL.Query().Get("version"))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to load field accuracy")
		return
	}
	respondJSON(w, http.StatusOK, acc)
}
