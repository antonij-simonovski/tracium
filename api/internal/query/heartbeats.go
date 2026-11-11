package query

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/tracium/api/internal/anomaly"
	"github.com/tracium/api/internal/model"
)

// Heartbeats replays the heartbeat check at each tick, as if run then: one
// read of the activity rollup covers every tick's four weeks of history.
func (r *ClickHouseRepository) Heartbeats(ctx context.Context, f MetricsFilter, ticks []int64) ([][]model.Anomaly, error) {
	out := make([][]model.Anomaly, len(ticks))
	if len(ticks) == 0 || !wants(f, "heartbeat") {
		return out, nil
	}
	hours := map[string][]int64{}
	first := time.UnixMilli(ticks[0]).Add(-heartbeatHistory)
	for end := f.End; end.After(first); end = end.Add(-heartbeatHistory) {
		g := f
		g.End = end
		clients, err := r.clientActivity(ctx, g)
		if err != nil {
			return nil, err
		}
		for _, c := range clients {
			hours[c.Client] = append(hours[c.Client], c.Hours...)
		}
	}
	for c, hs := range hours {
		slices.Sort(hs)
		hours[c] = slices.Compact(hs)
	}
	minSev := severityRank(f.AnomalyMinSeverity)
	historyMs := heartbeatHistory.Milliseconds()
	for i, t := range ticks {
		var clients []anomaly.Activity
		for c, hs := range hours {
			lo := sort.Search(len(hs), func(j int) bool { return hs[j] >= t-historyMs })
			hi := sort.Search(len(hs), func(j int) bool { return hs[j] >= t })
			if hi > lo && hs[hi-1]+anomaly.HourMs > t-silenceOpts.MinSilenceMs {
				continue
			}
			if hi > lo {
				clients = append(clients, anomaly.Activity{Client: c, Hours: hs[lo:hi]})
			}
		}
		o := silenceOpts
		o.NowMs = t
		for _, res := range anomaly.Silence(clients, o) {
			a := hourlyAnomaly("heartbeat", "", res.Result)
			a.Scope, a.Client = "client", res.Client
			if severityRank(a.Severity) < minSev {
				continue
			}
			a.Summary = hourlySummary(a, "", "")
			if res.Missed > 0 {
				a.Summary = fmt.Sprintf("Client %q has missed %d expected hours of activity; last seen %s.", res.Client, res.Missed,
					time.UnixMilli(res.LastSeenMs).UTC().Format("2006-01-02 15:04 UTC"))
			}
			a.TraceIDs = []string{}
			out[i] = append(out[i], a)
		}
	}
	return out, nil
}
