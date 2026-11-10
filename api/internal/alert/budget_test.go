package alert_test

import (
	"context"
	"testing"
	"time"

	"github.com/tracium/api/internal/alert"
	"github.com/tracium/api/testing/mocks"
)

type fakeCost struct {
	spend map[string]map[string]float64
	calls []string
}

func (f *fakeCost) Spend(_ context.Context, _, scope string, since time.Time) (map[string]float64, error) {
	f.calls = append(f.calls, scope+"@"+since.Format(time.DateOnly))
	return f.spend[scope+"@"+since.Format(time.DateOnly)], nil
}

func TestBudgets(t *testing.T) {
	now := time.Date(2026, 11, 10, 15, 0, 0, 0, time.UTC)
	day, month := "2026-11-10", "2026-11-01"
	cost := &fakeCost{spend: map[string]map[string]float64{
		"workspace@" + day:   {"": 79.99},
		"workspace@" + month: {"": 900},
		"client@" + day:      {"acme": 40, "globex": 55, "": 500, "tiny": 1},
		"workflow@" + day:    {"etl": 100, "chat": 10},
	}}
	store := &mocks.MockAlertStore{RuleSet: []alert.Rule{
		{WorkspaceID: "ws", Scope: "workspace", Period: "day", ThresholdUSD: 100, Enabled: true},
		{WorkspaceID: "ws", Scope: "workspace", Period: "month", ThresholdUSD: 1000, Enabled: true},
		{WorkspaceID: "ws", Scope: "client", Subject: "*", Period: "day", ThresholdUSD: 50, Enabled: true},
		{WorkspaceID: "ws", Scope: "client", Subject: "acme", Period: "day", ThresholdUSD: 45, Enabled: true},
		{WorkspaceID: "ws", Scope: "workflow", Subject: "etl", Period: "day", ThresholdUSD: 100, Enabled: true},
		{WorkspaceID: "ws", Scope: "workflow", Subject: "chat", Period: "day", ThresholdUSD: 1, Enabled: false},
		{WorkspaceID: "other", Scope: "workspace", Period: "day", ThresholdUSD: 1, Enabled: true},
	}}
	got, err := alert.NewBudgets(store, cost, alert.DefaultBudgetOptions()).Anomalies(context.Background(), "ws", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(cost.calls) != 4 {
		t.Fatalf("spend should be read once per scope and period, got %v", cost.calls)
	}
	want := map[string]string{
		"client/acme":   "warning",
		"client/globex": "critical",
		"workflow/etl":  "critical",
		"workspace/":    "warning",
	}
	if len(got) != len(want) {
		t.Fatalf("want %d signals, got %+v", len(want), got)
	}
	for _, a := range got {
		key := a.Scope + "/" + a.Workflow + a.Client
		if want[key] != a.Severity || a.Metric != alert.BudgetMetric {
			t.Errorf("%s: severity %s, want %s", key, a.Severity, want[key])
		}
		bucket := alert.PeriodStart("day", now)
		if a.Scope == "workspace" {
			bucket = alert.PeriodStart("month", now)
			if a.Observed != 900 || a.Expected != 1000 {
				t.Errorf("workspace signal should be the month rule at 90%%, got %+v", a)
			}
		}
		if a.BucketMs != bucket.UnixMilli() {
			t.Errorf("%s: bucket %d, want period start", key, a.BucketMs)
		}
	}

	quiet := &mocks.MockAlertStore{RuleSet: []alert.Rule{{WorkspaceID: "ws", Scope: "workflow", Subject: "*", Period: "day", ThresholdUSD: 1, Enabled: false}}}
	cost.calls = nil
	if got, _ := alert.NewBudgets(quiet, cost, alert.DefaultBudgetOptions()).Anomalies(context.Background(), "ws", now); len(got) != 0 || len(cost.calls) != 0 {
		t.Fatalf("disabled rules read nothing and signal nothing, got %+v %v", got, cost.calls)
	}
}

func TestPeriodStart(t *testing.T) {
	at := time.Date(2026, 3, 31, 23, 30, 0, 0, time.FixedZone("x", -5*3600))
	if got := alert.PeriodStart("day", at); !got.Equal(time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("day start is UTC: %v", got)
	}
	if got := alert.PeriodStart("month", at); !got.Equal(time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("month start is UTC: %v", got)
	}
}
