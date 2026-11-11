package incident

import (
	"testing"
	"time"

	"github.com/tracium/api/internal/model"
)

var now = time.Date(2026, 11, 10, 12, 30, 0, 0, time.UTC)

const hourMs = int64(time.Hour / time.Millisecond)

func hourly(metric, workflow, severity string, hoursAgo int64) model.Anomaly {
	bucket := now.Truncate(time.Hour).UnixMilli() - hoursAgo*hourMs
	return model.Anomaly{Metric: metric, Scope: "workflow", Workflow: workflow, BucketMs: bucket, Observed: 0.667, Severity: severity, Score: 10}
}

func openIncident(workflow, severity string, lastSignal time.Time, signals ...model.Anomaly) Incident {
	inc := Incident{ID: "inc-" + workflow, WorkspaceID: "ws", Scope: "workflow", Subject: workflow, Status: StatusOpen,
		Severity: severity, OpenedAt: lastSignal, LastSignalAt: lastSignal}
	for _, a := range signals {
		inc.Signals = append(inc.Signals, Signal{Metric: a.Metric, BucketMs: a.BucketMs, Severity: a.Severity, TraceIDs: []string{"t1"}})
	}
	return inc
}

func resolvedIncident(workflow string, ago time.Duration, signals ...model.Anomaly) Incident {
	inc := openIncident(workflow, "warning", now.Add(-ago), signals...)
	at := now.Add(-ago)
	inc.Status, inc.ResolvedAt = StatusResolved, &at
	return inc
}

func kinds(changes []Change) []string {
	var out []string
	for _, c := range changes {
		out = append(out, string(c.Kind)+":"+string(c.Event))
	}
	return out
}

func TestFold(t *testing.T) {
	trunc := hourly("truncation_rate", "sf-truncation", "warning", 0)
	until := now.Add(time.Hour)
	expired := now.Add(-time.Minute)
	cases := []struct {
		name      string
		anomalies []model.Anomaly
		state     State
		want      []string
	}{
		{"opens", []model.Anomaly{trunc}, State{}, []string{"create:opened"}},
		{"info only opens nothing", []model.Anomaly{hourly("truncation_rate", "a", "info", 0)}, State{}, nil},
		{"second signal joins", []model.Anomaly{hourly("tool_error_rate", "sf-truncation", "warning", 0)},
			State{Open: []Incident{openIncident("sf-truncation", "warning", now.Add(-time.Hour), trunc)}}, []string{"update:"}},
		{"severity rise escalates", []model.Anomaly{hourly("truncation_rate", "sf-truncation", "critical", 0)},
			State{Open: []Incident{openIncident("sf-truncation", "warning", now.Add(-time.Hour), trunc)}}, []string{"update:escalated"}},
		{"same severity again does not escalate", []model.Anomaly{hourly("truncation_rate", "sf-truncation", "critical", 0)},
			State{Open: []Incident{openIncident("sf-truncation", "critical", now.Add(-time.Hour), trunc)}}, []string{"update:"}},
		{"hourly not quiet yet", nil, State{Open: []Incident{openIncident("a", "warning", now.Add(-119*time.Minute), trunc)}}, nil},
		{"hourly resolves after 2h", nil, State{Open: []Incident{openIncident("a", "warning", now.Add(-2*time.Hour), trunc)}}, []string{"resolve:resolved"}},
		{"daily waits 26h", nil, State{Open: []Incident{openIncident("a", "warning", now.Add(-25*time.Hour), model.Anomaly{Metric: "cost"})}}, nil},
		{"daily resolves after 26h", nil, State{Open: []Incident{openIncident("a", "warning", now.Add(-26*time.Hour), model.Anomaly{Metric: "cost"})}}, []string{"resolve:resolved"}},
		{"heartbeat resolves once seen", nil, State{Open: []Incident{openIncident("c", "warning", now.Add(-time.Minute), model.Anomaly{Metric: "heartbeat"})}}, []string{"resolve:resolved"}},
		{"reopens within 24h", []model.Anomaly{trunc},
			State{Resolved: []Incident{resolvedIncident("sf-truncation", 2*time.Hour, hourly("truncation_rate", "sf-truncation", "warning", 2))}}, []string{"reopen:opened"}},
		{"evidence already resolved does not reopen", []model.Anomaly{trunc},
			State{Resolved: []Incident{resolvedIncident("sf-truncation", time.Hour, trunc)}}, nil},
		{"resolved over 24h ago opens a new one", []model.Anomaly{trunc},
			State{Resolved: []Incident{resolvedIncident("sf-truncation", 30*time.Hour, hourly("truncation_rate", "sf-truncation", "warning", 30))}}, []string{"create:opened"}},
		{"mute by subject", []model.Anomaly{trunc}, State{Mutes: []Mute{{Scope: "workflow", Subject: "sf-truncation"}}}, nil},
		{"mute by metric", []model.Anomaly{trunc}, State{Mutes: []Mute{{Scope: "workflow", Subject: "sf-truncation", Metric: "truncation_rate", Until: &until}}}, nil},
		{"mute of another metric", []model.Anomaly{trunc}, State{Mutes: []Mute{{Scope: "workflow", Subject: "sf-truncation", Metric: "refusal_rate"}}}, []string{"create:opened"}},
		{"expired mute", []model.Anomaly{trunc}, State{Mutes: []Mute{{Scope: "workflow", Subject: "sf-truncation", Until: &expired}}}, []string{"create:opened"}},
		{"old hourly ignored", []model.Anomaly{hourly("truncation_rate", "a", "critical", 4)}, State{}, nil},
		{"old daily ignored", []model.Anomaly{{Metric: "cost", Scope: "workflow", Workflow: "a", Severity: "critical",
			BucketMs: now.Truncate(24 * time.Hour).Add(-48 * time.Hour).UnixMilli()}}, State{}, nil},
		{"yesterday's daily is current", []model.Anomaly{{Metric: "cost", Scope: "workflow", Workflow: "a", Severity: "critical",
			BucketMs: now.Truncate(24 * time.Hour).Add(-24 * time.Hour).UnixMilli()}}, State{}, []string{"create:opened"}},
		{"old heartbeat is current", []model.Anomaly{{Metric: "heartbeat", Scope: "client", Client: "c", Severity: "warning", BucketMs: 0}}, State{}, []string{"create:opened"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := kinds(Fold("ws", now, c.anomalies, c.state, DefaultOptions()))
			if len(got) != len(c.want) || (len(got) > 0 && got[0] != c.want[0]) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestFoldGroupsBySubject(t *testing.T) {
	calls := hourly("llm_calls_per_run", "sf-loop", "critical", 0)
	repeat := hourly("tool_repeat", "sf-loop", "warning", 1)
	repeat.TraceIDs = []string{"run-1"}
	repeat.VersionChange = &model.VersionChange{Workflow: "sf-loop", Version: "v2"}
	other := hourly("truncation_rate", "sf-truncation", "warning", 0)
	changes := Fold("ws", now, []model.Anomaly{calls, repeat, other}, State{}, DefaultOptions())
	if len(changes) != 2 {
		t.Fatalf("got %d changes, want 2", len(changes))
	}
	inc := changes[0].Incident
	if inc.Subject != "sf-loop" || len(inc.Signals) != 2 || inc.Severity != "critical" || inc.VersionChange == nil || inc.WorkspaceID != "ws" {
		t.Fatalf("unexpected incident %+v", inc)
	}
	if want := "sf-loop: LLM calls per run at 1 (+1 more signal)"; inc.Title != want {
		t.Fatalf("title %q, want %q", inc.Title, want)
	}
	if inc.Signals[1].TraceIDs[0] != "run-1" {
		t.Fatalf("trace ids lost: %+v", inc.Signals[1])
	}
}

func TestFoldKeepsTraceIDsAndLeavesStateAlone(t *testing.T) {
	trunc := hourly("truncation_rate", "sf-truncation", "warning", 0)
	open := openIncident("sf-truncation", "warning", now.Add(-time.Hour), trunc)
	changes := Fold("ws", now, []model.Anomaly{trunc, hourly("refusal_rate", "sf-truncation", "warning", 0)}, State{Open: []Incident{open}}, DefaultOptions())
	inc := changes[0].Incident
	if len(inc.Signals) != 2 || inc.Signals[0].TraceIDs[0] != "t1" || !inc.LastSignalAt.Equal(now) {
		t.Fatalf("unexpected incident %+v", inc)
	}
	if len(open.Signals) != 1 {
		t.Fatal("fold changed the incident it was given")
	}
}

func TestTitle(t *testing.T) {
	sigs := []Signal{{Metric: "truncation_rate", Observed: 0.667, Severity: "warning"}}
	if got := Title("workflow", "sf-truncation", sigs); got != "sf-truncation: truncated LLM calls at 66.7%" {
		t.Fatal(got)
	}
	if got := Title("client", "acme", []Signal{{Metric: "heartbeat", Observed: 5, Severity: "warning"}}); got != "acme: silent for 5h" {
		t.Fatal(got)
	}
	sigs = append(sigs, Signal{Metric: "cost", Observed: 12, Severity: "critical"}, Signal{Metric: "runs", Severity: "info"})
	if got := Title("workspace", "", sigs); got != "Workspace: cost at $12.00 (+2 more signals)" {
		t.Fatal(got)
	}
}

func TestMutedReleaseFindings(t *testing.T) {
	mute := func(metric string) []Mute { return []Mute{{Scope: "workflow", Subject: "wf", Metric: metric}} }
	cases := []struct {
		mute, metric string
		want         bool
	}{
		{"release_regression", "release_regression:truncation_rate", true},
		{"release_regression", "release_regression:cost_per_run", true},
		{"release_regression", "truncation_rate", false},
		{"release_regression:truncation_rate", "release_regression:truncation_rate", true},
		{"release_regression:truncation_rate", "release_regression:cost_per_run", false},
		{"truncation_rate", "release_regression:truncation_rate", false},
	}
	for _, c := range cases {
		if got := Muted(mute(c.mute), "workflow", "wf", c.metric, now); got != c.want {
			t.Errorf("mute %s, metric %s: got %v, want %v", c.mute, c.metric, got, c.want)
		}
	}
	if !Current(model.Anomaly{Metric: "release_regression:cost_per_run", BucketMs: 1}, now, DefaultOptions()) {
		t.Error("release findings should always be current")
	}
}

func TestFoldGroupsServicesApart(t *testing.T) {
	a := model.Anomaly{Metric: "ingest_silence", Scope: "service", Service: "svc-a", BucketMs: now.UnixMilli(), Severity: "warning"}
	b := a
	b.Service = "svc-b"
	changes := Fold("ws", now, []model.Anomaly{a, b}, State{}, DefaultOptions())
	if len(changes) != 2 || changes[0].Incident.Subject == changes[1].Incident.Subject {
		t.Fatalf("want one incident per service, got %+v", changes)
	}
}
