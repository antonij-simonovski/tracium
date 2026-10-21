package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/internal/query"
)

// schemaLimit caps each list of workspace values the schema returns.
const schemaLimit = 50

// maxQueryBody caps a custom query request; a query is a few hundred bytes.
const maxQueryBody = 64 << 10

// QueryHandler serves structured custom queries over the caller's own
// telemetry, for the REST API and the MCP server alike.
type QueryHandler struct {
	traces  query.TraceRepository
	metrics query.MetricsRepository
	access  WorkspaceDirectory
	now     func() time.Time
}

// WorkspaceDirectory is WorkspaceAccess plus the workspaces' names, so an MCP
// client can choose which one to query.
type WorkspaceDirectory interface {
	WorkspaceAccess
	List(ctx context.Context, userID string) ([]model.Workspace, error)
}

func NewQueryHandler(traces query.TraceRepository, metrics query.MetricsRepository, access WorkspaceDirectory) *QueryHandler {
	return &QueryHandler{traces: traces, metrics: metrics, access: access, now: time.Now}
}

type queryRequest struct {
	query.Query
	WorkspaceID string `json:"workspace_id"`
}

// Query handles POST /v1/query.
func (h *QueryHandler) Query(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBody)
	var req queryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondDecodeError(w, err)
		return
	}
	scope, ok := resolveWorkspaceScope(w, r, h.access, req.WorkspaceID)
	if !ok {
		return
	}
	res, err := h.run(r.Context(), scope, req.Query)
	if errors.Is(err, query.ErrInvalidQuery) {
		respondError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	if err != nil {
		log.Printf("query: %v", err)
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to run query")
		return
	}
	respondJSON(w, http.StatusOK, res)
}

// Schema handles GET /v1/query/schema.
func (h *QueryHandler) Schema(w http.ResponseWriter, r *http.Request) {
	scope, ok := resolveWorkspaceScope(w, r, h.access, r.URL.Query().Get("workspace_id"))
	if !ok {
		return
	}
	schema, err := h.schema(r.Context(), scope)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL", "failed to load query schema")
		return
	}
	respondJSON(w, http.StatusOK, schema)
}

func (h *QueryHandler) run(ctx context.Context, scope []string, q query.Query) (model.QueryResult, error) {
	cols, err := h.metrics.QueryColumns(ctx)
	if err != nil {
		return model.QueryResult{}, err
	}
	if err := q.Validate(h.now(), cols); err != nil {
		return model.QueryResult{}, err
	}
	q.WorkspaceIDs = scope
	return h.metrics.RunQuery(ctx, q)
}

// schema pairs the static catalog with the values in the caller's last 30 days,
// so a query can name real workflows, models and attribute keys.
func (h *QueryHandler) schema(ctx context.Context, scope []string) (model.QuerySchema, error) {
	f, _ := query.ParseRange("30d", h.now())
	f.WorkspaceIDs = scope
	attrs, err := h.metrics.AttributeKeys(ctx, f, schemaLimit)
	if err != nil {
		return model.QuerySchema{}, err
	}
	workflows, err := h.metrics.TopWorkflows(ctx, f, schemaLimit)
	if err != nil {
		return model.QuerySchema{}, err
	}
	models, err := h.metrics.ModelCosts(ctx, f, schemaLimit)
	if err != nil {
		return model.QuerySchema{}, err
	}
	cols, err := h.metrics.QueryColumns(ctx)
	if err != nil {
		return model.QuerySchema{}, err
	}
	s := query.QueryCatalog(cols)
	s.AttributeKeys, s.Workflows, s.Models = attrs, []string{}, []string{}
	for _, wf := range workflows {
		s.Workflows = append(s.Workflows, wf.Name)
	}
	for _, m := range models {
		s.Models = append(s.Models, m.Name)
	}
	return s, nil
}
