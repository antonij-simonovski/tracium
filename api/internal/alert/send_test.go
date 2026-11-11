package alert

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tracium/api/extension"
	"github.com/tracium/api/internal/incident"
	"github.com/tracium/api/internal/model"
)

func TestBlockedRanges(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.1.2.3", "172.16.0.1", "172.31.255.255", "192.168.1.1", "169.254.169.254",
		"::1", "fc00::1", "fd00:ec2::254", "fe80::1", "0.0.0.0", "::", "100.100.100.200", "::ffff:127.0.0.1", "64:ff9b::a9fe:a9fe"} {
		if !blocked(netip.MustParseAddr(ip)) {
			t.Errorf("%s not blocked", ip)
		}
	}
	for _, ip := range []string{"8.8.8.8", "172.32.0.1", "2606:4700::1111"} {
		if blocked(netip.MustParseAddr(ip)) {
			t.Errorf("%s blocked", ip)
		}
	}
}

func TestValidateTarget(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		kind, target string
		allow        bool
		ok           bool
	}{
		{KindWebhook, "https://127.0.0.1/hook", false, false},
		{KindWebhook, "https://169.254.169.254/latest", false, false},
		{KindWebhook, "https://[::1]/hook", false, false},
		{KindWebhook, "https://localhost/hook", false, false},
		{KindWebhook, "http://8.8.8.8/hook", false, false},
		{KindWebhook, "ftp://example.com/x", true, false},
		{KindWebhook, "https://user:pw@8.8.8.8/x", false, false},
		{KindWebhook, "https://8.8.8.8/hook", false, true},
		{KindWebhook, "http://127.0.0.1:9000/hook", true, true},
		{KindSlack, "https://8.8.8.8/services/x", false, false},
		{KindSlack, "http://10.0.0.5/services/x", true, true},
		{KindEmail, "a@example.com, B <b@example.com>", false, true},
		{KindEmail, "not-an-address", false, false},
		{"sms", "x", false, false},
	} {
		_, err := ValidateTarget(ctx, tc.kind, tc.target, tc.allow)
		if (err == nil) != tc.ok {
			t.Errorf("%s %s allow=%v: err %v", tc.kind, tc.target, tc.allow, err)
		}
	}
	if got, _ := ValidateTarget(ctx, KindEmail, "a@example.com, B <b@example.com>", false); got != "a@example.com,b@example.com" {
		t.Errorf("emails normalised to %q", got)
	}
}

func TestDialBlocksPrivateAddresses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	s := httpSender{NewHTTPClient(false, time.Second), 4096}
	if err := s.post(context.Background(), srv.URL, []byte("{}"), http.Header{}); !errors.Is(err, errBlockedAddress) {
		t.Fatalf("loopback dial: %v", err)
	}
	if strings.Contains(s.post(context.Background(), srv.URL+"/secret-path", nil, http.Header{}).Error(), "secret-path") {
		t.Fatal("error leaks the URL")
	}
	s.client = NewHTTPClient(true, time.Second)
	if err := s.post(context.Background(), srv.URL, []byte("{}"), http.Header{}); err != nil {
		t.Fatalf("allowed: %v", err)
	}
}

func TestRedirectsNotFollowed(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/target" {
			hit = true
			return
		}
		http.Redirect(w, r, "/target", http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	s := httpSender{NewHTTPClient(true, time.Second), 4096}
	if err := s.post(context.Background(), srv.URL, nil, http.Header{}); err == nil || hit {
		t.Fatalf("redirect followed: err %v, hit %v", err, hit)
	}
}

func TestOversizedResponseCutOff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := []byte(strings.Repeat("x", 4096))
		for r.Context().Err() == nil {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer srv.Close()
	s := httpSender{NewHTTPClient(true, 3*time.Second), 4096}
	start := time.Now()
	if err := s.post(context.Background(), srv.URL, nil, http.Header{}); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("endless body was read")
	}
}

func sample() Message {
	at := time.Date(2026, 11, 10, 14, 0, 0, 0, time.UTC)
	return Render(incident.Event{Type: incident.EventOpened, Incident: incident.Incident{ID: "inc-1", WorkspaceID: "ws", Scope: "workflow", Subject: "sf-truncation",
		Severity: "critical", Title: "sf-truncation: truncated LLM calls at 66.7%", OpenedAt: at, UpdatedAt: at,
		VersionChange: &model.VersionChange{Workflow: "sf-truncation", Version: "v42", FirstSeenMs: at.Add(-19 * time.Minute).UnixMilli()},
		Signals: []incident.Signal{
			{Metric: "latency_p95", BucketMs: at.UnixMilli(), Severity: "warning", Summary: "p95 run latency rose to 9.0s"},
			{Metric: "truncation_rate", BucketMs: at.UnixMilli(), Severity: "critical", Summary: "Truncated LLM calls rose to 66.7%", TraceIDs: []string{"t1", "t2"}},
		}}}, "production", "https://tracium.example.com")
}

func TestWebhookSignatureVerifies(t *testing.T) {
	var got struct {
		body           []byte
		sig, ts, event string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.body, _ = io.ReadAll(r.Body)
		got.sig, got.ts, got.event = r.Header.Get("X-Tracium-Signature"), r.Header.Get("X-Tracium-Timestamp"), r.Header.Get("X-Tracium-Event")
	}))
	defer srv.Close()
	s := NewWebhookSender(NewHTTPClient(true, time.Second), 4096)
	if err := s.Send(context.Background(), Channel{Target: srv.URL, Secret: "whsec_x"}, sample()); err != nil {
		t.Fatal(err)
	}
	ts, _ := strconv.ParseInt(got.ts, 10, 64)
	if got.sig != Sign("whsec_x", ts, got.body) || got.event != "opened" {
		t.Fatalf("signature %q event %q", got.sig, got.event)
	}
	var body map[string]any
	if err := json.Unmarshal(got.body, &body); err != nil {
		t.Fatal(err)
	}
	inc := body["incident"].(map[string]any)
	if body["version"] != 1.0 || inc["url"] != "https://tracium.example.com/incidents/inc-1" || body["headline"] != nil {
		t.Fatalf("body %s", got.body)
	}
	if sigs := inc["signals"].([]any); sigs[0].(map[string]any)["metric"] != "truncation_rate" {
		t.Fatalf("signals not most severe first: %v", sigs)
	}
}

func TestSlackBody(t *testing.T) {
	var body struct {
		Text   string           `json:"text"`
		Blocks []map[string]any `json:"blocks"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}))
	defer srv.Close()
	if err := NewSlackSender(NewHTTPClient(true, time.Second), 4096).Send(context.Background(), Channel{Target: srv.URL}, sample()); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(body.Text, "[critical] sf-truncation") || body.Blocks[0]["type"] != "header" {
		t.Fatalf("slack body %+v", body)
	}
	last := body.Blocks[len(body.Blocks)-1]
	if last["type"] != "actions" || len(last["elements"].([]any)) != 3 {
		t.Fatalf("buttons %+v", last)
	}
}

func TestRenderText(t *testing.T) {
	text := sample().Text()
	for _, want := range []string{
		"[critical] sf-truncation: truncated LLM calls at 66.7%\nWorkspace: production\n\n• Truncated LLM calls rose to 66.7%\n• p95 run latency",
		"Likely cause: version v42 first seen Nov 10 13:41 UTC, 19 minutes before the first signal.",
		"Example runs: https://tracium.example.com/traces/t1 https://tracium.example.com/traces/t2",
		"Open incident: https://tracium.example.com/incidents/inc-1",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in\n%s", want, text)
		}
	}
	var signals []incident.Signal
	for i := range 7 {
		signals = append(signals, incident.Signal{Metric: strconv.Itoa(i), Severity: "warning", Summary: "s", TraceIDs: []string{"t"}})
	}
	at := time.Now()
	m := Render(incident.Event{Type: incident.EventEscalated, Incident: incident.Incident{ID: "i", Severity: "critical", Title: "x", Signals: signals}}, "", "")
	if m.Headline != "Escalated to critical: x" || len(m.Lines) != 6 || m.Lines[5] != "+2 more" || !strings.Contains(m.Text(), "Example runs: t\nIncident: i") {
		t.Fatalf("escalated without links: %+v\n%s", m, m.Text())
	}
	daily := []incident.Signal{{Metric: "cost", BucketMs: 1, Severity: "warning", Summary: "day 1"}, {Metric: "cost", BucketMs: 2, Severity: "critical", Summary: "day 2"}}
	if m := Render(incident.Event{Type: incident.EventOpened, Incident: incident.Incident{Signals: daily}}, "", ""); len(m.Lines) != 1 || m.Lines[0] != "day 2" || len(m.Incident.Signals) != 2 {
		t.Fatalf("one line per metric, payload keeps every bucket: %+v", m)
	}
	resolved := at.Add(2*time.Hour + 10*time.Minute)
	m = Render(incident.Event{Type: incident.EventResolved, Incident: incident.Incident{Title: "x", OpenedAt: at, ResolvedAt: &resolved}}, "", "")
	if m.Headline != "Resolved after 2h 10m: x" || len(m.Lines) != 0 {
		t.Fatalf("resolved %+v", m)
	}
}

func TestMask(t *testing.T) {
	if got := Mask(KindSlack, "https://hooks.slack.com/services/T0/B0/abcdA1b2"); got != "https://hooks.slack.com/…/…A1b2" {
		t.Fatal(got)
	}
}

func TestPermanentClasses(t *testing.T) {
	err := EmailSender{Mailer: extension.DisabledMailer{}}.Send(context.Background(), Channel{Target: "a@example.com"}, Message{})
	if !Permanent(err) {
		t.Fatal("mail disabled is retried")
	}
	if _, err := ValidateTarget(context.Background(), KindSlack, "https://example.com/x", false); !Permanent(err) {
		t.Fatal("non-Slack host is retried")
	}
	if Permanent(context.DeadlineExceeded) || Permanent(errors.New("connection refused")) {
		t.Fatal("transient error marked permanent")
	}
}
