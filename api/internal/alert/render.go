package alert

import (
	"cmp"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/tracium/api/internal/incident"
	"github.com/tracium/api/internal/model"
)

const maxSignalLines = 5

// Message is the rendered, sender-neutral content of one delivery. It is
// stored at enqueue time so every retry sends the same content. Version,
// Event and Incident are the webhook body.
type Message struct {
	Version   int             `json:"version"`
	Event     string          `json:"event"`
	Incident  PayloadIncident `json:"incident"`
	Workspace string          `json:"workspace"`
	Headline  string          `json:"headline"`
	Lines     []string        `json:"lines"`
	Cause     string          `json:"cause"`
	Runs      []Link          `json:"runs"`
	// ID is the delivery key, set before sending so mailers can deduplicate.
	ID string `json:"-"`
}

type Link struct {
	TraceID string `json:"trace_id"`
	URL     string `json:"url"`
}

type PayloadIncident struct {
	ID            string               `json:"id"`
	WorkspaceID   string               `json:"workspace_id"`
	Scope         string               `json:"scope"`
	Subject       string               `json:"subject"`
	Severity      string               `json:"severity"`
	Title         string               `json:"title"`
	OpenedAt      time.Time            `json:"opened_at"`
	ResolvedAt    *time.Time           `json:"resolved_at"`
	URL           string               `json:"url"`
	VersionChange *model.VersionChange `json:"version_change"`
	Signals       []PayloadSignal      `json:"signals"`
}

type PayloadSignal struct {
	Metric   string   `json:"metric"`
	BucketMs int64    `json:"bucket_ms"`
	Observed float64  `json:"observed"`
	Expected float64  `json:"expected"`
	Severity string   `json:"severity"`
	Summary  string   `json:"summary"`
	TraceIDs []string `json:"trace_ids"`
}

var severityRank = map[string]int{"info": 1, "warning": 2, "critical": 3}

// Render builds the message for an incident event. Links are omitted when
// publicURL is empty; the text then names the incident and runs by id.
func Render(e incident.Event, workspace, publicURL string) Message {
	inc := e.Incident
	signals := slices.Clone(inc.Signals)
	slices.SortStableFunc(signals, func(a, b incident.Signal) int { return severityRank[b.Severity] - severityRank[a.Severity] })
	link := func(path string) string {
		if publicURL == "" {
			return ""
		}
		return publicURL + path
	}
	m := Message{Version: 1, Event: string(e.Type), Workspace: workspace, Lines: []string{}, Runs: []Link{}, Incident: PayloadIncident{
		ID: inc.ID, WorkspaceID: inc.WorkspaceID, Scope: inc.Scope, Subject: inc.Subject, Severity: inc.Severity, Title: inc.Title,
		OpenedAt: inc.OpenedAt, ResolvedAt: inc.ResolvedAt, URL: link("/incidents/" + url.PathEscape(inc.ID)),
		VersionChange: inc.VersionChange, Signals: []PayloadSignal{},
	}}
	seen, first := map[string]bool{}, int64(0)
	for i, s := range signals {
		ids := s.TraceIDs
		if ids == nil {
			ids = []string{}
		}
		m.Incident.Signals = append(m.Incident.Signals, PayloadSignal{Metric: s.Metric, BucketMs: s.BucketMs, Observed: s.Observed,
			Expected: s.Expected, Severity: s.Severity, Summary: s.Summary, TraceIDs: ids})
		if i == 0 || s.BucketMs < first {
			first = s.BucketMs
		}
		for _, id := range ids {
			if len(m.Runs) < 3 && !seen[id] {
				seen[id] = true
				m.Runs = append(m.Runs, Link{TraceID: id, URL: link("/traces/" + url.PathEscape(id))})
			}
		}
	}

	signals = incident.PerMetric(signals)

	switch e.Type {
	case incident.EventResolved:
		at := time.Now()
		if inc.ResolvedAt != nil {
			at = *inc.ResolvedAt
		}
		m.Headline = fmt.Sprintf("Resolved after %s: %s", span(at.Sub(inc.OpenedAt)), inc.Title)
		m.Runs = []Link{}
		return m
	case incident.EventEscalated:
		m.Headline = fmt.Sprintf("Escalated to %s: %s", inc.Severity, inc.Title)
	default:
		m.Headline = fmt.Sprintf("[%s] %s", inc.Severity, inc.Title)
	}
	for i, s := range signals {
		if i == maxSignalLines {
			m.Lines = append(m.Lines, fmt.Sprintf("+%d more", len(signals)-maxSignalLines))
			break
		}
		m.Lines = append(m.Lines, s.Summary)
	}
	if vc := inc.VersionChange; vc != nil && len(signals) > 0 {
		seenAt := time.UnixMilli(vc.FirstSeenMs).UTC()
		if gap := time.UnixMilli(first).Sub(seenAt); gap >= 0 {
			m.Cause = fmt.Sprintf("Likely cause: version %s first seen %s, %s before the first signal.", vc.Version, seenAt.Format("Jan 2 15:04 UTC"), span(gap))
		} else {
			m.Cause = fmt.Sprintf("Version %s first seen %s, %s after the first signal.", vc.Version, seenAt.Format("Jan 2 15:04 UTC"), span(-gap))
		}
	}
	return m
}

// span formats a duration as "19 minutes" or "2h 10m".
func span(d time.Duration) string {
	d = d.Round(time.Minute)
	switch {
	case d < time.Minute:
		return "less than a minute"
	case d < time.Hour:
		if d == time.Minute {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
	}
}

// Text is the plain-text message, used for email and as Slack's fallback.
func (m Message) Text() string {
	var b strings.Builder
	b.WriteString(m.Headline + "\n")
	if m.Workspace != "" {
		b.WriteString("Workspace: " + m.Workspace + "\n")
	}
	if len(m.Lines) > 0 {
		b.WriteString("\n")
		for _, l := range m.Lines {
			if strings.HasPrefix(l, "+") {
				b.WriteString(l + "\n")
			} else {
				b.WriteString("• " + l + "\n")
			}
		}
	}
	if m.Cause != "" {
		b.WriteString("\n" + m.Cause + "\n")
	}
	if len(m.Runs) > 0 {
		refs := make([]string, len(m.Runs))
		for i, r := range m.Runs {
			refs[i] = cmp.Or(r.URL, r.TraceID)
		}
		b.WriteString("Example runs: " + strings.Join(refs, " ") + "\n")
	}
	if m.Incident.URL != "" {
		b.WriteString("Open incident: " + m.Incident.URL + "\n")
	} else if m.Incident.ID != "" {
		b.WriteString("Incident: " + m.Incident.ID + "\n")
	}
	return b.String()
}
