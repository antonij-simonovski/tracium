package review

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tracium/api/internal/anomaly"
)

var accNow = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

func ver(version string, reviews, correct, unflagged, unflaggedCorrect int64) VersionReviews {
	return VersionReviews{Workflow: "extract", Version: version, Reviews: reviews, Correct: correct, Unflagged: unflagged,
		UnflaggedCorrect: unflaggedCorrect, FirstAt: accNow.Add(-48 * time.Hour), LastAt: accNow.Add(-time.Hour)}
}

func TestAccuracyDrop(t *testing.T) {
	o := DefaultAccuracyOptions()
	stale := ver("v2", 30, 10, 0, 0)
	stale.LastAt = accNow.Add(-25 * time.Hour)
	staleBase := ver("v1", 30, 28, 0, 0)
	staleBase.LastAt = stale.LastAt
	cases := []struct {
		name       string
		prev, next VersionReviews
		fires      bool
		severity   string
		likeLike   bool
	}{
		{"big drop, all reviews", ver("v1", 30, 28, 0, 0), ver("v2", 30, 15, 0, 0), true, "critical", false},
		{"moderate drop is warning", ver("v1", 100, 90, 0, 0), ver("v2", 100, 72, 0, 0), true, "warning", false},
		{"drop under 10 points", ver("v1", 400, 360, 0, 0), ver("v2", 400, 328, 0, 0), false, "", false},
		{"10 point drop on few reviews is noise", ver("v1", 20, 18, 0, 0), ver("v2", 20, 16, 0, 0), false, "", false},
		{"improvement never fires", ver("v1", 30, 15, 0, 0), ver("v2", 30, 28, 0, 0), false, "", false},
		{"stale reviews", staleBase, stale, false, "", false},
		// Flagged reviews drag v2 down, but its unflagged runs are as good as v1's.
		{"like with like hides flagged bias", ver("v1", 40, 36, 30, 27), ver("v2", 60, 36, 30, 27), false, "", true},
		{"like with like drop", ver("v1", 40, 38, 30, 29), ver("v2", 40, 20, 30, 14), true, "critical", true},
		{"falls back when one side lacks unflagged", ver("v1", 40, 38, 30, 29), ver("v2", 40, 20, 19, 9), true, "critical", false},
	}
	for _, c := range cases {
		a, ok := AccuracyDrop(c.prev, c.next, accNow, o)
		if ok != c.fires {
			t.Errorf("%s: fired = %v (%+v)", c.name, ok, a)
			continue
		}
		if !ok {
			continue
		}
		if a.Severity != c.severity || a.Metric != "review_accuracy" || a.Scope != "workflow" || a.VersionChange.Version != "v2" || a.Direction != "down" {
			t.Errorf("%s: %+v", c.name, a)
		}
		if got := strings.Contains(a.Summary, "unflagged runs only"); got != c.likeLike {
			t.Errorf("%s: summary %q", c.name, a.Summary)
		}
	}
}

type fakeAccuracy []VersionReviews

func (f fakeAccuracy) RunAccuracy(context.Context, string) ([]VersionReviews, error) { return f, nil }

type fakeFindings map[string]anomaly.Finding

func (f fakeFindings) AddFinding(_ context.Context, _, workflow, version string, fd anomaly.Finding) error {
	f[workflow+"@"+version] = fd
	return nil
}

func TestAccuracyWatchComparesNewestTwoVersionsWithEnoughReviews(t *testing.T) {
	old, prev, next, few := ver("v0", 50, 10, 0, 0), ver("v1", 30, 28, 0, 0), ver("v2", 30, 12, 0, 0), ver("v3", 5, 0, 0, 0)
	other := ver("v1", 30, 10, 0, 0)
	other.Workflow = "solo"
	findings := fakeFindings{}
	got, err := NewAccuracyWatch(fakeAccuracy{old, prev, next, few, other}, findings, DefaultAccuracyOptions()).Anomalies(context.Background(), "ws", accNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Workflow != "extract" || got[0].VersionChange.Version != "v2" || got[0].Expected < 0.9 {
		t.Fatalf("anomalies = %+v", got)
	}
	if f, ok := findings["extract@v2"]; !ok || f.Verdict != anomaly.Regressed || f.Metric != "review_accuracy" {
		t.Errorf("findings = %+v", findings)
	}
}
