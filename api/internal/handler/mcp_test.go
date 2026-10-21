package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/internal/query"
	"github.com/tracium/api/testing/mocks"
)

type directory struct{ allowAllAccess }

func (directory) List(context.Context, string) ([]model.Workspace, error) {
	return []model.Workspace{{ID: "ws-test", Name: "Test", Env: "production"}, {ID: "ws-gone", Name: "Left"}}, nil
}

func mcpCall(t *testing.T, h *QueryHandler, body string) (int, map[string]any) {
	t.Helper()
	rr := httptest.NewRecorder()
	serveAuthed(h.MCP, rr, httptest.NewRequest(http.MethodPost, "/v1/mcp", strings.NewReader(body)))
	var out map[string]any
	if rr.Body.Len() > 0 {
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatalf("reply is not JSON: %s", rr.Body.String())
		}
	}
	return rr.Code, out
}

func TestMCPProtocol(t *testing.T) {
	metrics := &mocks.MockMetricsRepository{
		QueryResult: model.QueryResult{Columns: []string{"cost"}, Rows: []map[string]any{{"cost": 1.5}}},
		QueryCols:   query.Columns{"cost_usd": "number", "input": "text"},
	}
	h := NewQueryHandler(mocks.NewMockTraceRepository(), metrics, directory{})

	code, out := mcpCall(t, h, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
	result, _ := out["result"].(map[string]any)
	if code != 200 || result["protocolVersion"] != "2025-06-18" || result["capabilities"] == nil {
		t.Fatalf("initialize: %d %v", code, out)
	}
	_, out = mcpCall(t, h, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`)
	if out["result"].(map[string]any)["protocolVersion"] != mcpProtocolVersions[0] {
		t.Fatalf("unsupported version not answered with ours: %v", out)
	}

	if code, _ := mcpCall(t, h, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); code != http.StatusAccepted {
		t.Fatalf("notification: %d, want 202", code)
	}

	_, out = mcpCall(t, h, `{"jsonrpc":"2.0","id":"a","method":"tools/list"}`)
	tools := out["result"].(map[string]any)["tools"].([]any)
	if len(tools) != len(mcpTools) || out["id"] != "a" {
		t.Fatalf("tools/list: %v", out)
	}
	for _, tool := range mcpTools {
		var schema map[string]any
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil || schema["type"] != "object" {
			t.Fatalf("%s input schema is not a JSON object schema: %v", tool.Name, err)
		}
	}

	_, out = mcpCall(t, h, `{"jsonrpc":"2.0","id":"w","method":"tools/call","params":{"name":"list_workspaces"}}`)
	got, _ := json.Marshal(toolJSON(t, out))
	if string(got) != `{"workspaces":[{"env":"production","id":"ws-test","name":"Test"}]}` {
		t.Fatalf("list_workspaces = %s", got)
	}

	_, out = mcpCall(t, h, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"query","arguments":{"metrics":["cost"],"dimensions":["attr.team"]}}}`)
	result = out["result"].(map[string]any)
	if result["isError"] != nil || toolJSON(t, out)["rows"] == nil {
		t.Fatalf("query call: %v", out)
	}
	if got := metrics.LastQuery; got.Dimensions[0] != "attr.team" || got.WorkspaceIDs[0] != "ws-test" {
		t.Fatalf("query reached repository as %+v", got)
	}

	_, out = mcpCall(t, h, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"query","arguments":{"columns":["input","cost_usd"],"filters":[{"field":"cost_usd","op":"gt","value":0.5}]}}}`)
	if out["result"].(map[string]any)["isError"] != nil {
		t.Fatalf("row query on discovered columns: %v", out)
	}
	if got := metrics.LastQuery; got.Columns[1] != "cost_usd" || got.Filters[0].Value != "0.5" || got.Limit != 50 {
		t.Fatalf("row query reached repository as %+v", got)
	}

	_, out = mcpCall(t, h, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"query","arguments":{"metrics":["price"]}}}`)
	result = out["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if result["isError"] != true || !strings.Contains(text, "valid: calls, cost") {
		t.Fatalf("invalid query should be a recoverable tool error: %v", out)
	}

	_, out = mcpCall(t, h, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"query","arguments":{"metrics":["cost"],"workspace_id":"ws-other"}}}`)
	if out["result"].(map[string]any)["isError"] != true {
		t.Fatalf("foreign workspace was not refused: %v", out)
	}

	traces := mocks.NewMockTraceRepository()
	traces.AddTrace(model.Trace{TraceID: "t1", Name: "checkout", WorkspaceID: "ws-test", TotalCostUSD: 0.5},
		[]model.Span{{TraceID: "t1", SpanID: "s1", Model: "gpt-4o", Input: "hello", WorkspaceID: "ws-test"}})
	h = NewQueryHandler(traces, metrics, directory{})
	_, out = mcpCall(t, h, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"get_trace","arguments":{"trace_id":"t1","fields":["name"],"span_fields":["model","input"]}}}`)
	got, _ = json.Marshal(toolJSON(t, out))
	if string(got) != `{"span_page":1,"spans":[{"input":"hello","model":"gpt-4o"}],"total_spans":1,"trace":{"name":"checkout"}}` {
		t.Fatalf("get_trace with fields = %s", got)
	}
	traces.AddTrace(model.Trace{TraceID: "t2", WorkspaceID: "ws-test"}, nil)
	_, out = mcpCall(t, h, `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"list_traces","arguments":{"limit":1}}}`)
	if r := toolJSON(t, out); r["page"] != 1.0 || r["next_page"] != 2.0 {
		t.Fatalf("list_traces paging: %v", r)
	}
	_, out = mcpCall(t, h, `{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"get_trace","arguments":{"trace_id":"t1","span_page":2}}}`)
	if r := toolJSON(t, out); len(r["spans"].([]any)) != 0 || r["next_span_page"] != nil {
		t.Fatalf("get_trace past the last span page: %v", r)
	}
	_, out = mcpCall(t, h, `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"list_traces","arguments":{"fields":["trace_id","price"]}}}`)
	if r := out["result"].(map[string]any); r["isError"] != true || !strings.Contains(fmt.Sprint(r["content"]), "valid: trace_id") {
		t.Fatalf("unknown field should be a tool error: %v", out)
	}

	_, out = mcpCall(t, h, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"drop_tables"}}`)
	if out["error"].(map[string]any)["code"] != -32602.0 {
		t.Fatalf("unknown tool: %v", out)
	}
	_, out = mcpCall(t, h, `{"jsonrpc":"2.0","id":6,"method":"resources/list"}`)
	if out["error"].(map[string]any)["code"] != -32601.0 {
		t.Fatalf("unknown method: %v", out)
	}
	if code, out := mcpCall(t, h, `not json`); code != 400 || out["error"].(map[string]any)["code"] != -32700.0 {
		t.Fatalf("parse error: %d %v", code, out)
	}
}

func TestMCPGetTraceClipsContent(t *testing.T) {
	traces := mocks.NewMockTraceRepository()
	h := NewQueryHandler(traces, &mocks.MockMetricsRepository{}, directory{})
	_, out := mcpCall(t, h, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_trace","arguments":{"trace_id":"missing"}}}`)
	if out["result"].(map[string]any)["isError"] != true {
		t.Fatalf("missing trace: %v", out)
	}
	traces.AddTrace(model.Trace{TraceID: "t1", WorkspaceID: "ws-test"}, []model.Span{{SpanID: "s1", WorkspaceID: "ws-test"}})
	_, out = mcpCall(t, h, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_trace","arguments":{"trace_id":"t1","span_page":72057594037927937}}}`)
	if spans := toolJSON(t, out)["spans"].([]any); len(spans) != 0 {
		t.Fatalf("overflowing span_page: %v", out)
	}
	if got := query.ClipText(strings.Repeat("é", 2005)); !strings.HasSuffix(got, "…[truncated]") || len([]rune(got)) != 2000+len([]rune("…[truncated]")) {
		t.Fatalf("clip kept %d runes", len([]rune(got)))
	}
}

// toolJSON decodes a tool result's text content, where the JSON payload is.
func toolJSON(t *testing.T, out map[string]any) map[string]any {
	t.Helper()
	text := out["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	var v map[string]any
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		t.Fatalf("tool text is not JSON: %q", text)
	}
	return v
}

func TestMCPRefusesForeignOrigins(t *testing.T) {
	h := NewQueryHandler(mocks.NewMockTraceRepository(), &mocks.MockMetricsRepository{}, directory{})
	for origin, want := range map[string]int{
		"":                       http.StatusOK,
		"https://tracium.test":   http.StatusOK,
		"https://evil.test":      http.StatusForbidden,
		"http://tracium.test.ev": http.StatusForbidden,
		"null":                   http.StatusForbidden,
	} {
		r := httptest.NewRequest(http.MethodPost, "https://tracium.test/v1/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		rr := httptest.NewRecorder()
		serveAuthed(h.MCP, rr, r)
		if rr.Code != want {
			t.Errorf("origin %q: %d, want %d", origin, rr.Code, want)
		}
	}
}

func TestMCPToolsAreAnnotatedReadOnly(t *testing.T) {
	h := NewQueryHandler(mocks.NewMockTraceRepository(), &mocks.MockMetricsRepository{}, directory{})
	_, out := mcpCall(t, h, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	for _, tool := range out["result"].(map[string]any)["tools"].([]any) {
		tool := tool.(map[string]any)
		a := tool["annotations"].(map[string]any)
		if tool["title"] == "" || a["readOnlyHint"] != true || a["destructiveHint"] != false || a["openWorldHint"] != false {
			t.Errorf("%s: title %q annotations %v", tool["name"], tool["title"], a)
		}
	}
	_, out = mcpCall(t, h, `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`)
	if v := out["result"].(map[string]any)["protocolVersion"]; v == "2025-03-26" {
		t.Fatal("2025-03-26 allows batches and must not be negotiated")
	}
}
