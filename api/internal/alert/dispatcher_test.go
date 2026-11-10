package alert_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tracium/api/internal/alert"
	"github.com/tracium/api/internal/incident"
	"github.com/tracium/api/testing/mocks"
)

type fakeSender struct {
	sent []alert.Message
	err  error
}

func (f *fakeSender) Send(_ context.Context, _ alert.Channel, m alert.Message) error {
	f.sent = append(f.sent, m)
	return f.err
}

func setup(chans ...alert.Channel) (*alert.Dispatcher, *mocks.MockAlertStore, *fakeSender) {
	store := &mocks.MockAlertStore{ChannelSet: chans, Names: map[string]string{"ws": "production"}}
	s := &fakeSender{}
	return alert.NewDispatcher(store, map[string]alert.Sender{alert.KindEmail: s}, alert.DefaultOptions()), store, s
}

func channel(id, minSeverity string, notifyResolved bool) alert.Channel {
	return alert.Channel{ID: id, WorkspaceID: "ws", Kind: alert.KindEmail, Target: "a@example.com", MinSeverity: minSeverity, NotifyResolved: notifyResolved, Enabled: true}
}

func event(typ incident.EventType, severity string, at time.Time) incident.Event {
	inc := incident.Incident{ID: "inc", WorkspaceID: "ws", Severity: severity, Title: "wf: x", OpenedAt: at, UpdatedAt: at}
	if typ == incident.EventResolved {
		inc.ResolvedAt = &at
	}
	return incident.Event{Type: typ, Incident: inc}
}

func TestNotifyFiltersChannels(t *testing.T) {
	off := channel("off", "warning", true)
	off.Enabled = false
	other := channel("other-ws", "warning", true)
	other.WorkspaceID = "ws2"
	d, store, _ := setup(channel("warn", "warning", true), channel("crit", "critical", true), channel("quiet", "warning", false), off, other)
	ctx := context.Background()
	at := time.Date(2026, 11, 10, 14, 0, 0, 0, time.UTC)

	if err := d.Notify(ctx, event(incident.EventOpened, "warning", at)); err != nil {
		t.Fatal(err)
	}
	if got := channelsOf(store.Queue); got != "warn,quiet" {
		t.Fatalf("warning opened queued for %s", got)
	}
	store.Queue = nil
	if err := d.Notify(ctx, event(incident.EventResolved, "critical", at)); err != nil {
		t.Fatal(err)
	}
	if got := channelsOf(store.Queue); got != "warn,crit" {
		t.Fatalf("critical resolved queued for %s", got)
	}
	if store.Queue[0].Payload.Workspace != "production" || store.Queue[0].Payload.Event != "resolved" {
		t.Fatalf("payload not rendered at enqueue: %+v", store.Queue[0].Payload)
	}
}

func channelsOf(ds []alert.Delivery) string {
	s := ""
	for i, d := range ds {
		if i > 0 {
			s += ","
		}
		s += d.ChannelID
	}
	return s
}

func TestNotifyIsIdempotentAndReopenGetsNextSeq(t *testing.T) {
	d, store, _ := setup(channel("c", "warning", true))
	ctx := context.Background()
	at := time.Date(2026, 11, 10, 14, 0, 0, 0, time.UTC)
	for range 2 {
		if err := d.Notify(ctx, event(incident.EventOpened, "warning", at)); err != nil {
			t.Fatal(err)
		}
	}
	if len(store.Queue) != 1 {
		t.Fatalf("repeated notify queued %d deliveries", len(store.Queue))
	}
	if err := d.Notify(ctx, event(incident.EventOpened, "warning", at.Add(3*time.Hour))); err != nil {
		t.Fatal(err)
	}
	if len(store.Queue) != 2 || store.Queue[1].Seq != 1 {
		t.Fatalf("reopen: %+v", store.Queue)
	}
}

func TestFlushRetriesThenFails(t *testing.T) {
	d, store, sender := setup(channel("c", "warning", true))
	ctx := context.Background()
	now := time.Date(2026, 11, 10, 14, 0, 0, 0, time.UTC)
	alert.SetClock(d, func() time.Time { return now })
	if err := d.Notify(ctx, event(incident.EventOpened, "critical", now)); err != nil {
		t.Fatal(err)
	}
	sender.err = errors.New("destination answered 500")
	for i, wait := range []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute, time.Hour} {
		if err := d.Flush(ctx); err != nil {
			t.Fatal(err)
		}
		q := store.Queue[0]
		if q.Status != alert.StatusPending || q.Attempts != i+1 || !q.NextAttemptAt.Equal(now.Add(wait)) || q.LastError == "" {
			t.Fatalf("attempt %d: %+v", i+1, q)
		}
		if err := d.Flush(ctx); err != nil || len(sender.sent) != i+1 {
			t.Fatalf("flush before backoff sent again: %d sends", len(sender.sent))
		}
		now = now.Add(wait)
	}
	if err := d.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if q := store.Queue[0]; q.Status != alert.StatusFailed || q.Attempts != 5 {
		t.Fatalf("after 5 attempts: %+v", q)
	}
	if err := d.Flush(ctx); err != nil || len(sender.sent) != 5 {
		t.Fatalf("failed delivery sent again: %d sends", len(sender.sent))
	}
}

func TestFlushSendsOnce(t *testing.T) {
	d, store, sender := setup(channel("c", "warning", true))
	ctx := context.Background()
	if err := d.Notify(ctx, event(incident.EventOpened, "critical", time.Now())); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := d.Flush(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if len(sender.sent) != 1 || store.Queue[0].Status != alert.StatusSent || store.Queue[0].SentAt == nil {
		t.Fatalf("sent %d, queue %+v", len(sender.sent), store.Queue)
	}
	if sender.sent[0].ID != "inc/c/opened/0" {
		t.Fatalf("message id %q is not the delivery key", sender.sent[0].ID)
	}
}

func TestFlushFailsDisabledChannel(t *testing.T) {
	d, store, sender := setup(channel("c", "warning", true))
	ctx := context.Background()
	if err := d.Notify(ctx, event(incident.EventOpened, "critical", time.Now())); err != nil {
		t.Fatal(err)
	}
	store.ChannelSet[0].Enabled = false
	if err := d.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 0 || store.Queue[0].Status != alert.StatusFailed {
		t.Fatalf("disabled channel: sent %d, %+v", len(sender.sent), store.Queue[0])
	}
}

func TestEmailChannelRejectedWhenMailDisabled(t *testing.T) {
	d, _, _ := setup()
	if _, err := d.Validate(context.Background(), alert.KindEmail, "a@example.com"); !errors.Is(err, alert.ErrEmailDisabled) {
		t.Fatalf("got %v", err)
	}
}
