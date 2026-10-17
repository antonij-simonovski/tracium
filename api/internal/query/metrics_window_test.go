package query

import (
	"strings"
	"testing"
	"time"
)

func TestParseRange(t *testing.T) {
	now := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		rng        string
		wantSpan   time.Duration
		wantBucket time.Duration
	}{
		{"24h", 24 * time.Hour, time.Hour},
		{"7d", 7 * 24 * time.Hour, 24 * time.Hour},
		{"30d", 30 * 24 * time.Hour, 24 * time.Hour},
		{"90d", 90 * 24 * time.Hour, 24 * time.Hour},
		{"1y", 365 * 24 * time.Hour, 24 * time.Hour},
	}
	for _, tt := range tests {
		f, err := ParseRange(tt.rng, now)
		if err != nil {
			t.Fatalf("ParseRange(%q): unexpected error %v", tt.rng, err)
		}
		if !f.End.Equal(now) {
			t.Errorf("%s: End = %v, want %v", tt.rng, f.End, now)
		}
		if got := f.End.Sub(f.Start); got != tt.wantSpan {
			t.Errorf("%s: span = %v, want %v", tt.rng, got, tt.wantSpan)
		}
		// Previous window is equal-length and immediately precedes the current one.
		if got := f.Start.Sub(f.PrevStart); got != tt.wantSpan {
			t.Errorf("%s: prev span = %v, want %v", tt.rng, got, tt.wantSpan)
		}
		if f.Bucket != tt.wantBucket {
			t.Errorf("%s: bucket = %v, want %v", tt.rng, f.Bucket, tt.wantBucket)
		}
	}
}

// UseRollup routes long windows (>30d) to the daily rollup and keeps shorter
// ones on raw spans.
func TestUseRollup(t *testing.T) {
	now := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	for _, rng := range []string{"24h", "7d", "30d"} {
		f, _ := ParseRange(rng, now)
		if f.UseRollup() {
			t.Errorf("%s: UseRollup = true, want false", rng)
		}
	}
	for _, rng := range []string{"90d", "1y"} {
		f, _ := ParseRange(rng, now)
		if !f.UseRollup() {
			t.Errorf("%s: UseRollup = false, want true", rng)
		}
	}
}

func TestParseRangeInvalid(t *testing.T) {
	if _, err := ParseRange("5y", time.Now()); err == nil {
		t.Fatal("ParseRange(\"5y\"): expected error, got nil")
	}
}

func TestKPIDeltaType(t *testing.T) {
	tests := []struct {
		name        string
		cur, prev   float64
		higherIsBad bool
		wantDelta   float64
		wantType    string
	}{
		{"cost up is bad", 2, 1, true, 1.0, "bad"},
		{"runs up is good", 110, 100, false, 0.1, "good"},
		{"latency down is good", 2, 4, true, -0.5, "good"},
		{"flat is neutral", 5, 5, true, 0, "neutral"},
		{"no prior period is neutral, not classified", 5, 0, true, 0, "neutral"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := kpi(tt.cur, tt.prev, tt.higherIsBad)
			if k.Value != tt.cur {
				t.Errorf("Value = %v, want %v", k.Value, tt.cur)
			}
			if k.Delta != tt.wantDelta {
				t.Errorf("Delta = %v, want %v", k.Delta, tt.wantDelta)
			}
			if k.DeltaType != tt.wantType {
				t.Errorf("DeltaType = %q, want %q", k.DeltaType, tt.wantType)
			}
		})
	}
}

func TestWindowClause(t *testing.T) {
	// window carries no source predicate now — the relation (tracium.calls, or
	// both cost relations) supplies that. It renders only the time bounds plus the
	// business (user) and access (workspace) filters.
	// An empty workspace scope is a hard deny (match nothing), never "match
	// everything" — the read-side access boundary.
	clause, args := window("", nil, 100, 200)
	if clause != "start_time_ms >= ? AND start_time_ms < ? AND 1 = 0" {
		t.Errorf("clause = %q", clause)
	}
	if len(args) != 2 || args[0] != int64(100) || args[1] != int64(200) {
		t.Errorf("args = %v, want [100 200]", args)
	}

	// A user adds its own bound after the time range; the empty scope still denies.
	clause, args = window("acme", nil, 100, 200)
	if clause != "start_time_ms >= ? AND start_time_ms < ? AND user_id = ? AND 1 = 0" {
		t.Errorf("user clause = %q", clause)
	}
	if len(args) != 3 || args[2] != "acme" {
		t.Errorf("args = %v", args)
	}

	// A single accessible workspace compiles to an equality, after the user.
	clause, args = window("acme", []string{"ws_1"}, 100, 200)
	if clause != "start_time_ms >= ? AND start_time_ms < ? AND user_id = ? AND workspace_id = ?" {
		t.Errorf("workspace clause = %q", clause)
	}
	if len(args) != 4 || args[2] != "acme" || args[3] != "ws_1" {
		t.Errorf("args = %v", args)
	}

	// Several accessible workspaces compile to an IN over the allowed set.
	clause, args = window("", []string{"a", "b"}, 100, 200)
	if clause != "start_time_ms >= ? AND start_time_ms < ? AND workspace_id IN (?,?)" {
		t.Errorf("multi-workspace clause = %q", clause)
	}
	if len(args) != 4 || args[2] != "a" || args[3] != "b" {
		t.Errorf("args = %v", args)
	}
}

// The reconciled cost union reads BOTH typed relations — calls for span cost,
// usage_metrics for metric cost — and never names `source`. Its args are the
// window args once per branch, so a caller binds them calls-first.
func TestReconciledCostUnionReadsBothRelations(t *testing.T) {
	clause, args := window("acme", []string{"ws_1"}, 100, 200)
	sql, both := reconciledCostUnion(clause, args)

	if !strings.Contains(sql, "tracium.calls") || !strings.Contains(sql, "tracium.usage_metrics") {
		t.Errorf("union does not read both relations:\n%s", sql)
	}
	if strings.Contains(sql, "source") {
		t.Errorf("union should not mention source:\n%s", sql)
	}
	if !strings.Contains(sql, "cost_usd AS span_cost") || !strings.Contains(sql, "cost_usd AS metric_cost") ||
		!strings.Contains(sql, "if(model = '', cost_usd, 0) AS reported_cost") {
		t.Errorf("union does not tag cost by relation:\n%s", sql)
	}
	// Window args are repeated once per branch, in order.
	if len(both) != 2*len(args) {
		t.Fatalf("args = %v, want the window args twice (%d)", both, 2*len(args))
	}
	for i := range args {
		if both[i] != args[i] || both[i+len(args)] != args[i] {
			t.Errorf("args not duplicated per branch: %v", both)
		}
	}
}
