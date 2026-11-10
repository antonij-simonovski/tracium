package incident

import (
	"strings"
	"testing"
	"time"

	"github.com/tracium/api/internal/model"
)

func TestFoldAccuracyDrop(t *testing.T) {
	o := DefaultOptions()
	// The bucket is the version's first review, days old; the source decides currency.
	drop := model.Anomaly{Metric: AccuracyMetric, Scope: "workflow", Workflow: "wf", BucketMs: now.Add(-5 * 24 * time.Hour).UnixMilli(),
		Observed: 0.5, Expected: 0.95, Severity: "warning", VersionChange: &model.VersionChange{Workflow: "wf", Version: "v2"}}
	changes := Fold("ws", now, []model.Anomaly{drop}, State{}, o)
	if len(changes) != 1 || changes[0].Kind != ChangeCreate || changes[0].Incident.VersionChange.Version != "v2" {
		t.Fatalf("changes = %+v", changes)
	}
	if title := changes[0].Incident.Title; !strings.Contains(title, "reviewed accuracy at 50.0%") {
		t.Errorf("title = %q", title)
	}
	open := changes[0].Incident
	open.ID = "inc"
	if got := kinds(Fold("ws", now.Add(time.Minute), nil, State{Open: []Incident{open}}, o)); len(got) != 1 || got[0] != "resolve:resolved" {
		t.Errorf("recovery = %v, want an immediate resolve", got)
	}
	muted := State{Mutes: []Mute{{Scope: "workflow", Subject: "wf", Metric: AccuracyMetric}}}
	if got := Fold("ws", now, []model.Anomaly{drop}, muted, o); len(got) != 0 {
		t.Errorf("muted = %v", kinds(got))
	}
}
