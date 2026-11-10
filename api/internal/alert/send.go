package alert

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/tracium/api/extension"
)

// Sender delivers one message to one channel.
type Sender interface {
	Send(ctx context.Context, ch Channel, m Message) error
}

var errBlockedAddress = errors.New("destination address is private, loopback or link-local; set ALERTS_ALLOW_PRIVATE_URLS=true to allow it")

var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("64:ff9b::/96"),
}

// blocked reports whether ip is loopback, private, link-local, unique-local or
// otherwise not a public unicast address. Cloud metadata endpoints sit in these ranges.
func blocked(ip netip.Addr) bool {
	ip = ip.Unmap()
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast() || !ip.IsValid() {
		return true
	}
	for _, p := range blockedPrefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// NewHTTPClient returns the client senders post with. Unless allowPrivate, it
// refuses blocked addresses in the dialer, so the check applies to every
// address actually dialled, not only the first DNS answer. It never follows
// redirects and ignores proxy settings.
func NewHTTPClient(allowPrivate bool, timeout time.Duration) *http.Client {
	d := &net.Dialer{Timeout: timeout}
	if !allowPrivate {
		d.Control = func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil || blocked(ap.Addr()) {
				return errBlockedAddress
			}
			return nil
		}
	}
	return &http.Client{
		Timeout:       timeout,
		Transport:     &http.Transport{DialContext: d.DialContext, TLSHandshakeTimeout: timeout, ResponseHeaderTimeout: timeout},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// ValidateTarget checks a channel target and returns it normalised. URLs must
// be https with a public host (Slack: hooks.slack.com) unless allowPrivate.
func ValidateTarget(ctx context.Context, kind, target string, allowPrivate bool) (string, error) {
	target = strings.TrimSpace(target)
	if kind == KindEmail {
		var out []string
		for _, part := range strings.Split(target, ",") {
			a, err := mail.ParseAddress(strings.TrimSpace(part))
			if err != nil {
				return "", fmt.Errorf("%q is not an email address", strings.TrimSpace(part))
			}
			out = append(out, a.Address)
		}
		if len(out) > 20 {
			return "", errors.New("at most 20 email addresses per channel")
		}
		return strings.Join(out, ","), nil
	}
	if kind != KindSlack && kind != KindWebhook {
		return "", errors.New("kind must be slack, webhook or email")
	}
	u, err := url.Parse(target)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return "", errors.New("target must be an https URL")
	}
	if allowPrivate {
		return u.String(), nil
	}
	if u.Scheme != "https" {
		return "", errors.New("target must be an https URL")
	}
	host := u.Hostname()
	if kind == KindSlack && host != "hooks.slack.com" {
		return "", errors.New("slack target must be a https://hooks.slack.com/ incoming webhook URL")
	}
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return "", fmt.Errorf("host %q does not resolve", host)
	}
	for _, a := range addrs {
		if blocked(a) {
			return "", errBlockedAddress
		}
	}
	return u.String(), nil
}

// Mask hides all but the scheme, host and last four characters of a URL target.
func Mask(kind, target string) string {
	if kind == KindEmail {
		return target
	}
	u, err := url.Parse(target)
	if err != nil {
		return "…"
	}
	tail := target
	if len(tail) > 4 {
		tail = tail[len(tail)-4:]
	}
	return u.Scheme + "://" + u.Host + "/…/…" + tail
}

type httpSender struct {
	client   *http.Client
	maxBytes int64
}

// post sends body and fails on anything but a 2xx. Errors never carry the URL.
func (s httpSender) post(ctx context.Context, target string, body []byte, header http.Header) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return errors.New("invalid target URL")
	}
	req.Header = header
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Tracium-Alerts/1")
	resp, err := s.client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, s.maxBytes))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("destination answered %d", resp.StatusCode)
	}
	return nil
}

type SlackSender struct{ httpSender }

func NewSlackSender(c *http.Client, maxBytes int64) SlackSender {
	return SlackSender{httpSender{c, maxBytes}}
}

type slackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (s SlackSender) Send(ctx context.Context, ch Channel, m Message) error {
	body, err := json.Marshal(slackBody(m))
	if err != nil {
		return err
	}
	return s.post(ctx, ch.Target, body, http.Header{})
}

func slackBody(m Message) map[string]any {
	header := m.Headline
	if r := []rune(header); len(r) > 150 {
		header = string(r[:147]) + "..."
	}
	blocks := []map[string]any{{"type": "header", "text": slackText{"plain_text", header}}}
	section := func(text string) {
		blocks = append(blocks, map[string]any{"type": "section", "text": slackText{"mrkdwn", text}})
	}
	if m.Workspace != "" {
		section("Workspace: *" + m.Workspace + "*")
	}
	if len(m.Lines) > 0 {
		lines := make([]string, len(m.Lines))
		for i, l := range m.Lines {
			lines[i] = "• " + l
		}
		section(strings.Join(lines, "\n"))
	}
	if m.Cause != "" {
		section(m.Cause)
	}
	var buttons []map[string]any
	var plainRuns []string
	for i, r := range m.Runs {
		if r.URL == "" {
			plainRuns = append(plainRuns, "`"+r.TraceID+"`")
			continue
		}
		buttons = append(buttons, map[string]any{"type": "button", "text": slackText{"plain_text", "Example run " + strconv.Itoa(i+1)}, "url": r.URL})
	}
	if len(plainRuns) > 0 {
		section("Example runs: " + strings.Join(plainRuns, " "))
	}
	if m.Incident.URL != "" {
		buttons = append([]map[string]any{{"type": "button", "style": "primary", "text": slackText{"plain_text", "Open incident"}, "url": m.Incident.URL}}, buttons...)
	} else if m.Incident.ID != "" {
		section("Incident: `" + m.Incident.ID + "`")
	}
	if len(buttons) > 0 {
		blocks = append(blocks, map[string]any{"type": "actions", "elements": buttons})
	}
	return map[string]any{"text": m.Text(), "blocks": blocks}
}

type WebhookSender struct {
	httpSender
	now func() time.Time
}

func NewWebhookSender(c *http.Client, maxBytes int64) WebhookSender {
	return WebhookSender{httpSender{c, maxBytes}, time.Now}
}

// Sign is the X-Tracium-Signature value for body sent at ts.
func Sign(secret string, ts int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", ts)
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func (s WebhookSender) Send(ctx context.Context, ch Channel, m Message) error {
	body, err := json.Marshal(struct {
		Version  int             `json:"version"`
		Event    string          `json:"event"`
		Incident PayloadIncident `json:"incident"`
	}{m.Version, m.Event, m.Incident})
	if err != nil {
		return err
	}
	ts := s.now().Unix()
	h := http.Header{}
	h.Set("X-Tracium-Event", m.Event)
	h.Set("X-Tracium-Timestamp", strconv.FormatInt(ts, 10))
	h.Set("X-Tracium-Signature", Sign(ch.Secret, ts, body))
	return s.post(ctx, ch.Target, body, h)
}

type EmailSender struct{ Mailer extension.Mailer }

func (s EmailSender) Send(ctx context.Context, ch Channel, m Message) error {
	var errs []error
	for _, to := range strings.Split(ch.Target, ",") {
		if err := s.Mailer.Send(ctx, extension.Message{ID: m.ID + "/" + to, To: to, Subject: m.Headline, Text: m.Text()}); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
