package query

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/tracium/api/internal/model"
)

type detectorSpan struct {
	trace, parent, name, kind, user, workflow, flag, finish string
	startMs, durMs, input, cacheRead                        int64
}

func insertDetectorSpans(t *testing.T, r *ClickHouseRepository, ws string, spans []detectorSpan) {
	t.Helper()
	tx, err := r.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO tracium.spans (trace_id, span_id, parent_span_id, name, kind,
    start_time_ms, end_time_ms, duration_ms, user_id, workspace_id, workflow_name, silent_failure,
    finish_reason, input_tokens, cache_read_tokens)`)
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range spans {
		if _, err := stmt.Exec(s.trace, fmt.Sprint(i), s.parent, s.name, s.kind, s.startMs, s.startMs+s.durMs, s.durMs,
			s.user, ws, s.workflow, s.flag, s.finish, s.input, s.cacheRead); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestSilentFailureDetectorsFromClickHouse(t *testing.T) {
	r, ws := newTestRepository(t)
	now := time.Now().UTC()
	hour := now.Truncate(time.Hour).UnixMilli() - 3600_000
	today := now.Truncate(24 * time.Hour).UnixMilli()

	var spans []detectorSpan
	run := func(trace string, start int64, calls, tools int, flag func(i int) (string, string), cache int64, toolFlag func(i int) string) {
		for i := 0; i < calls; i++ {
			f, finish := flag(i)
			spans = append(spans, detectorSpan{trace: trace, parent: "root", name: "chat", kind: "llm", workflow: "agent",
				flag: f, finish: finish, startMs: start + int64(i), durMs: 5, input: 1000, cacheRead: cache})
		}
		for i := 0; i < tools; i++ {
			spans = append(spans, detectorSpan{trace: trace, parent: "root", name: "search", kind: "tool", workflow: "agent",
				flag: toolFlag(i), startMs: start + 100 + int64(i), durMs: 5})
		}
	}
	healthy := func(int) (string, string) { return "", "stop" }
	noToolErr := func(int) string { return "" }
	for h := 1; h <= 24; h++ {
		for k := 0; k < 5; k++ {
			run(fmt.Sprintf("base-%d-%d", h, k), hour-int64(h)*3600_000+int64(k)*1000, 4, 1, healthy, 800, noToolErr)
		}
	}
	for k := 0; k < 5; k++ {
		run(fmt.Sprintf("now-%d", k), hour+int64(k)*1000, 4, 1, healthy, 0, noToolErr)
	}
	run("runaway", hour+10_000, 40, 15,
		func(i int) (string, string) {
			if i < 10 {
				return "truncated", "length"
			}
			return "", "stop"
		}, 0,
		func(i int) string {
			if i < 6 {
				return "tool_error"
			}
			return ""
		})

	for d := int64(1); d <= 20; d++ {
		for k := int64(0); k < 30; k++ {
			spans = append(spans, detectorSpan{trace: fmt.Sprintf("lat-%d-%d", d, k), name: "agent", workflow: "slow",
				startMs: today - d*day + k*1000, durMs: 1000})
		}
	}
	for k := int64(0); k < 30; k++ {
		spans = append(spans, detectorSpan{trace: fmt.Sprintf("lat-today-%d", k), name: "agent", workflow: "slow",
			startMs: today + k*1000, durMs: 10_000})
	}

	for d := int64(3); d <= 22; d++ {
		spans = append(spans, detectorSpan{trace: fmt.Sprintf("steady-%d", d), name: "agent", workflow: "cron", user: "steady",
			startMs: today - d*day + 9*3600_000, durMs: 10})
	}
	for d := int64(6); d <= 27; d += 7 {
		spans = append(spans, detectorSpan{trace: fmt.Sprintf("weekly-%d", d), name: "agent", workflow: "cron", user: "weekly",
			startMs: today - d*day + 9*3600_000, durMs: 10})
	}
	insertDetectorSpans(t, r, ws, spans)

	f, _ := ParseRange("7d", now)
	f.WorkspaceIDs = []string{ws}
	got, err := r.Anomalies(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	find := func(metric, target string) *model.Anomaly {
		for i, a := range got {
			if a.Metric == metric && (a.Workflow == target || a.Client == target) {
				return &got[i]
			}
		}
		return nil
	}
	for _, want := range []struct{ metric, target string }{
		{"truncation_rate", "agent"},
		{"tool_error_rate", "agent"},
		{"llm_calls_per_run", "agent"},
		{"tool_repeat", "agent"},
		{"cache_hit_ratio", "agent"},
		{"latency_p95", "slow"},
		{"heartbeat", "steady"},
	} {
		a := find(want.metric, want.target)
		if a == nil {
			t.Errorf("no %s anomaly for %s", want.metric, want.target)
			continue
		}
		t.Logf("%s %s: %s", a.Severity, a.Metric, a.Summary)
	}
	if a := find("llm_calls_per_run", "agent"); a != nil && (a.Observed != 40 || a.BucketMs != hour) {
		t.Errorf("llm_calls_per_run = %+v, want 40 calls in hour %d", a, hour)
	}
	if a := find("heartbeat", "weekly"); a != nil {
		t.Errorf("weekly client flagged: %s", a.Summary)
	}
	if a := find("refusal_rate", "agent"); a != nil {
		t.Errorf("refusal flagged without refusals: %s", a.Summary)
	}
}
