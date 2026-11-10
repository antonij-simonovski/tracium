package release_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/internal/query"
	"github.com/tracium/api/internal/release"
	"github.com/tracium/api/testing/mocks"
)

type window struct {
	version  string
	from, to time.Time
}

type fakeStats struct {
	runs  int64
	next  model.VersionStats
	prev  func(from time.Time) model.VersionStats
	calls []window
}

func (s *fakeStats) NewVersions(_ context.Context, f query.MetricsFilter, _ time.Time) ([]model.NewVersion, error) {
	return []model.NewVersion{{Workflow: "agent", Version: "v2", PrevVersion: "v1", FirstSeenMs: shipped.UnixMilli(), Runs: s.runs}}, nil
}

func (s *fakeStats) VersionRunStats(_ context.Context, _ query.MetricsFilter, _, version string, from, to time.Time) (model.VersionStats, error) {
	s.calls = append(s.calls, window{version, from, to})
	if version == "v2" {
		return s.next, nil
	}
	return s.prev(from), nil
}

var shipped = time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)

func healthy(runs int64) model.VersionStats {
	return model.VersionStats{Runs: runs, LLMCalls: runs * 3, TruncatedCalls: runs * 3 / 100, CostPerRunP50: 0.02, LatencyP95: 2000, LLMCallsPerRunP50: 3, OutputTokensPerRunP50: 300}
}

func truncating(runs int64) model.VersionStats {
	s := healthy(runs)
	s.TruncatedCalls, s.CostPerRunP50 = runs*3/4, 0.04
	return s
}

func steadyPrev(time.Time) model.VersionStats { return healthy(400) }

func tick(t *testing.T, tr *release.Tracker, at time.Time) []model.Anomaly {
	t.Helper()
	out, err := tr.Anomalies(context.Background(), "ws", at)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestLifecycle(t *testing.T) {
	o := release.DefaultOptions()

	t.Run("pending until 30 runs, then regressed", func(t *testing.T) {
		store := &mocks.MockReleaseStore{}
		stats := &fakeStats{runs: 20, next: truncating(20), prev: steadyPrev}
		tr := release.NewTracker(stats, store, o)
		if got := tick(t, tr, shipped.Add(time.Hour)); len(got) != 0 || store.Reports[0].Status != release.StatusPending {
			t.Fatalf("20 runs after 1h: got %+v, report %+v", got, store.Reports[0])
		}
		stats.runs, stats.next = 38, truncating(38)
		got := tick(t, tr, shipped.Add(90*time.Minute))
		r := store.Reports[0]
		if r.Status != release.StatusFinal || r.Verdict != "regressed" || r.Runs != 38 || r.PrevRuns != 400 {
			t.Fatalf("report = %+v", r)
		}
		if len(got) != 1 || got[0].Metric != "release_regression" || got[0].Workflow != "agent" || got[0].VersionChange.Version != "v2" || got[0].Severity != "critical" {
			t.Fatalf("anomaly = %+v", got)
		}
		if !strings.HasPrefix(got[0].Summary, "v2 regressed against v1: truncated LLM calls 24.6% on v2, 1.0% on v1; median cost per run $0.0400 on v2, $0.0200 on v1 (38 runs against 400).") {
			t.Errorf("summary = %q", got[0].Summary)
		}
		if len(tick(t, tr, shipped.Add(90*time.Minute+23*time.Hour))) != 1 || len(tick(t, tr, shipped.Add(90*time.Minute+24*time.Hour))) != 0 {
			t.Error("a regression should stay current for 24h after grading, then stop")
		}
	})

	t.Run("10 runs after 2h is graded ok", func(t *testing.T) {
		store := &mocks.MockReleaseStore{}
		stats := &fakeStats{runs: 10, next: healthy(10), prev: steadyPrev}
		tr := release.NewTracker(stats, store, o)
		tick(t, tr, shipped.Add(119*time.Minute))
		if store.Reports[0].Status != release.StatusPending {
			t.Fatal("graded before 2h with 10 runs")
		}
		if got := tick(t, tr, shipped.Add(2*time.Hour)); len(got) != 0 || store.Reports[0].Verdict != "ok" {
			t.Fatalf("got %+v, report %+v", got, store.Reports[0])
		}
	})

	t.Run("insufficient after 24h under 10 runs", func(t *testing.T) {
		store := &mocks.MockReleaseStore{}
		stats := &fakeStats{runs: 9, next: truncating(9), prev: steadyPrev}
		tr := release.NewTracker(stats, store, o)
		tick(t, tr, shipped.Add(23*time.Hour))
		if store.Reports[0].Status != release.StatusPending {
			t.Fatal("finalized before 24h")
		}
		if got := tick(t, tr, shipped.Add(24*time.Hour)); len(got) != 0 || store.Reports[0].Verdict != "insufficient" || len(stats.calls) != 0 {
			t.Fatalf("got %+v, report %+v, stats read %d times", got, store.Reports[0], len(stats.calls))
		}
	})

	t.Run("one recheck turns ok into regressed", func(t *testing.T) {
		store := &mocks.MockReleaseStore{}
		stats := &fakeStats{runs: 30, next: healthy(30), prev: steadyPrev}
		tr := release.NewTracker(stats, store, o)
		tick(t, tr, shipped.Add(time.Hour))
		stats.runs, stats.next = 299, truncating(299)
		tick(t, tr, shipped.Add(2*time.Hour))
		if store.Reports[0].Verdict != "ok" {
			t.Fatal("rechecked before 300 runs")
		}
		stats.runs, stats.next = 300, truncating(300)
		if got := tick(t, tr, shipped.Add(3*time.Hour)); len(got) != 1 || store.Reports[0].Verdict != "regressed" || store.Reports[0].Runs != 300 {
			t.Fatalf("got %+v, report %+v", got, store.Reports[0])
		}
	})

	t.Run("an ok recheck is the last one", func(t *testing.T) {
		store := &mocks.MockReleaseStore{}
		stats := &fakeStats{runs: 30, next: healthy(30), prev: steadyPrev}
		tr := release.NewTracker(stats, store, o)
		tick(t, tr, shipped.Add(time.Hour))
		tick(t, tr, shipped.Add(25*time.Hour))
		if r := store.Reports[0]; r.Verdict != "ok" || !r.Rechecked {
			t.Fatalf("report = %+v", r)
		}
		stats.next, stats.calls = truncating(300), nil
		tick(t, tr, shipped.Add(30*time.Hour))
		if store.Reports[0].Verdict != "ok" || len(stats.calls) != 0 {
			t.Fatal("graded a third time")
		}
	})

	t.Run("regressed never flips back", func(t *testing.T) {
		store := &mocks.MockReleaseStore{}
		stats := &fakeStats{runs: 30, next: truncating(30), prev: steadyPrev}
		tr := release.NewTracker(stats, store, o)
		tick(t, tr, shipped.Add(time.Hour))
		stats.runs, stats.next, stats.calls = 500, healthy(500), nil
		tick(t, tr, shipped.Add(26*time.Hour))
		if store.Reports[0].Verdict != "regressed" || len(stats.calls) != 0 {
			t.Fatalf("report = %+v after %d reads", store.Reports[0], len(stats.calls))
		}
	})

	t.Run("previous window widens to 7 days when thin", func(t *testing.T) {
		store := &mocks.MockReleaseStore{}
		stats := &fakeStats{runs: 30, next: healthy(30), prev: func(from time.Time) model.VersionStats {
			if from.Before(shipped.Add(-25 * time.Hour)) {
				return healthy(200)
			}
			return healthy(12)
		}}
		tick(t, release.NewTracker(stats, store, o), shipped.Add(time.Hour))
		if store.Reports[0].PrevRuns != 200 || len(stats.calls) != 3 || !stats.calls[2].from.Equal(shipped.Add(-7*24*time.Hour)) {
			t.Fatalf("report %+v, reads %+v", store.Reports[0], stats.calls)
		}
		if !stats.calls[0].to.Equal(shipped.Add(time.Hour)) {
			t.Errorf("new version read to %v, want now", stats.calls[0].to)
		}
	})
}
