package alert

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/tracium/api/extension"
	"github.com/tracium/api/internal/incident"
	"github.com/tracium/api/internal/model"
)

var ErrEmailDisabled = fmt.Errorf("email alerts are unavailable: %w", extension.ErrMailDisabled)

// Dispatcher implements incident.Notifier by queueing one delivery per
// matching channel, and sends the queue with Flush.
type Dispatcher struct {
	store   Store
	senders map[string]Sender
	opts    Options
	now     func() time.Time
	wake    chan struct{}
}

func NewDispatcher(store Store, senders map[string]Sender, opts Options) *Dispatcher {
	return &Dispatcher{store: store, senders: senders, opts: opts, now: time.Now, wake: make(chan struct{}, 1)}
}

// NewSenders returns the production senders for every channel kind.
func NewSenders(mailer extension.Mailer, opts Options) map[string]Sender {
	c := NewHTTPClient(opts.AllowPrivateURLs, opts.Timeout)
	return map[string]Sender{
		KindSlack:   NewSlackSender(c, opts.MaxResponseBytes),
		KindWebhook: NewWebhookSender(c, opts.MaxResponseBytes),
		KindEmail:   EmailSender{Mailer: mailer},
	}
}

func (d *Dispatcher) EmailEnabled() bool { return d.opts.EmailEnabled }

// Validate checks a new channel's target and returns it normalised.
func (d *Dispatcher) Validate(ctx context.Context, kind, target string) (string, error) {
	if kind == KindEmail && !d.opts.EmailEnabled {
		return "", ErrEmailDisabled
	}
	return ValidateTarget(ctx, kind, target, d.opts.AllowPrivateURLs)
}

func wants(ch Channel, e incident.Event) bool {
	return ch.Enabled && severityRank[e.Incident.Severity] >= severityRank[ch.MinSeverity] &&
		(e.Type != incident.EventResolved || ch.NotifyResolved)
}

// Notify queues the event for every enabled channel of the incident's
// workspace that wants it. It sends nothing itself.
func (d *Dispatcher) Notify(ctx context.Context, e incident.Event) error {
	chans, err := d.store.Channels(ctx, e.Incident.WorkspaceID)
	if err != nil {
		return err
	}
	now := d.now()
	at := e.Incident.UpdatedAt
	if e.Type == incident.EventResolved && e.Incident.ResolvedAt != nil {
		at = *e.Incident.ResolvedAt
	}
	if at.IsZero() {
		at = now
	}
	var ds []Delivery
	var m *Message
	for _, ch := range chans {
		if !wants(ch, e) {
			continue
		}
		if m == nil {
			name, err := d.store.WorkspaceName(ctx, e.Incident.WorkspaceID)
			if err != nil {
				return err
			}
			r := Render(e, name, d.opts.PublicURL)
			m = &r
		}
		ds = append(ds, Delivery{IncidentID: e.Incident.ID, ChannelID: ch.ID, Event: string(e.Type), EventAt: at,
			Status: StatusPending, Payload: *m, NextAttemptAt: now})
	}
	if len(ds) == 0 {
		return nil
	}
	return d.enqueue(ctx, ds)
}

// EventHealthyRelease is a release notice, delivered like an incident event.
const EventHealthyRelease = "release_healthy"

// HealthyRelease queues "v42 of checkout-agent looks healthy after 30 runs."
// for every enabled channel of the workspace that asked for release notices.
func (d *Dispatcher) HealthyRelease(ctx context.Context, workspaceID, workflow, version string, runs int64, at time.Time) error {
	chans, err := d.store.Channels(ctx, workspaceID)
	if err != nil {
		return err
	}
	var ds []Delivery
	var m *Message
	for _, ch := range chans {
		if !ch.Enabled || !ch.NotifyHealthyReleases {
			continue
		}
		if m == nil {
			name, err := d.store.WorkspaceName(ctx, workspaceID)
			if err != nil {
				return err
			}
			headline := fmt.Sprintf("%s of %s looks healthy after %d runs.", version, workflow, runs)
			m = &Message{Version: 1, Event: EventHealthyRelease, Workspace: name, Headline: headline, Lines: []string{}, Runs: []Link{},
				Incident: PayloadIncident{WorkspaceID: workspaceID, Scope: "workflow", Subject: workflow, Severity: "info", Title: headline, OpenedAt: at,
					VersionChange: &model.VersionChange{Workflow: workflow, Version: version}, Signals: []PayloadSignal{}}}
		}
		ds = append(ds, Delivery{IncidentID: "release:" + workflow + "@" + version, ChannelID: ch.ID, Event: EventHealthyRelease, EventAt: at,
			Status: StatusPending, Payload: *m, NextAttemptAt: d.now()})
	}
	if len(ds) == 0 {
		return nil
	}
	return d.enqueue(ctx, ds)
}

func (d *Dispatcher) enqueue(ctx context.Context, ds []Delivery) error {
	if err := d.store.Enqueue(ctx, ds); err != nil {
		return err
	}
	select {
	case d.wake <- struct{}{}:
	default:
	}
	return nil
}

// Run flushes on every interval, and as soon as Notify queues something,
// until ctx is cancelled.
func (d *Dispatcher) Run(ctx context.Context) {
	t := time.NewTicker(d.opts.FlushInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-d.wake:
		}
		if err := d.Flush(ctx); err != nil && ctx.Err() == nil {
			log.Printf("alert dispatcher: %v", err)
		}
	}
}

// Flush sends every due delivery once.
func (d *Dispatcher) Flush(ctx context.Context) error {
	for {
		batch, err := d.store.Claim(ctx, d.now(), d.opts.Lease, d.opts.BatchSize)
		if err != nil {
			return err
		}
		for _, del := range batch {
			if err := d.deliver(ctx, del); err != nil {
				return err
			}
		}
		if len(batch) < d.opts.BatchSize || ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

func (d *Dispatcher) deliver(ctx context.Context, del Delivery) error {
	del.Attempts++
	var err error
	if !del.Channel.Enabled {
		err = permanent(errors.New("channel disabled"))
	} else {
		del.Payload.ID = fmt.Sprintf("%s/%s/%s/%d", del.IncidentID, del.ChannelID, del.Event, del.Seq)
		err = d.send(ctx, del.Channel, del.Payload)
	}
	now := d.now()
	var ra retryAfterError
	switch {
	case err == nil:
		del.Status, del.LastError, del.SentAt = StatusSent, "", &now
	case Permanent(err):
		del.Status, del.LastError = StatusFailed, err.Error()+" (permanent, not retried)"
	case del.Attempts > len(d.opts.Backoff):
		del.Status, del.LastError = StatusFailed, err.Error()
	default:
		wait := d.opts.Backoff[del.Attempts-1]
		if errors.As(err, &ra) && ra.after > 0 {
			wait = min(ra.after, d.opts.Backoff[len(d.opts.Backoff)-1])
		}
		del.Status, del.LastError, del.NextAttemptAt = StatusPending, err.Error(), now.Add(wait)
	}
	if err != nil {
		log.Printf("alert delivery: channel %s incident %s %s attempt %d: %v", del.ChannelID, del.IncidentID, del.Event, del.Attempts, err)
	}
	return d.store.Finish(ctx, del)
}

// send re-checks the target, so channels stored under a looser setting obey the current one.
func (d *Dispatcher) send(ctx context.Context, ch Channel, m Message) error {
	s, ok := d.senders[ch.Kind]
	if !ok {
		return fmt.Errorf("no sender for %s channels", ch.Kind)
	}
	if ch.Kind != KindEmail {
		if _, err := ValidateTarget(ctx, ch.Kind, ch.Target, d.opts.AllowPrivateURLs); err != nil {
			return err
		}
	}
	return s.Send(ctx, ch, m)
}

// Test sends a sample message to ch now.
func (d *Dispatcher) Test(ctx context.Context, ch Channel) error {
	name, err := d.store.WorkspaceName(ctx, ch.WorkspaceID)
	if err != nil {
		return err
	}
	now := d.now().UTC()
	m := Message{Version: 1, Event: "test", Workspace: name, ID: "test/" + uuid.NewString(),
		Headline: "Test alert from Tracium",
		Lines:    []string{"Channel " + ch.Name + " is set up. Real alerts list the signals, the likely cause and example runs."},
		Runs:     []Link{},
		Incident: PayloadIncident{ID: "test", WorkspaceID: ch.WorkspaceID, Severity: "info", Title: "Test alert", OpenedAt: now, Signals: []PayloadSignal{}},
	}
	return d.send(ctx, ch, m)
}
