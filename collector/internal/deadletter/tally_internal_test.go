package deadletter

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"testing"
)

type flakyTallyWriter struct {
	fail        bool
	written     map[Rejection]uint64
	hadDeadline bool
}

func (w *flakyTallyWriter) WriteRejected(ctx context.Context, tally map[Rejection]uint64) error {
	_, w.hadDeadline = ctx.Deadline()
	if w.fail {
		return errors.New("clickhouse down")
	}
	for r, n := range tally {
		w.written[r] += n
	}
	return nil
}

func (w *flakyTallyWriter) Close() error { return nil }

func TestTallyFlushKeepsCountsOnWriteFailure(t *testing.T) {
	w := &flakyTallyWriter{fail: true, written: map[Rejection]uint64{}}
	s := &TallyStore{writer: w, logger: slog.Default(), tally: map[Rejection]uint64{}}
	r := Rejection{WorkspaceID: "ws-1", Code: "invalid_timestamp"}
	s.add(r, 2)

	s.flush()
	if !w.hadDeadline {
		t.Fatal("flush wrote without a deadline")
	}
	s.add(r, 1)
	w.fail = false
	s.flush()

	if w.written[r] != 3 {
		t.Fatalf("written = %d, want 3", w.written[r])
	}
}

func TestTallyIsBounded(t *testing.T) {
	s := &TallyStore{tally: map[Rejection]uint64{}}
	for i := 0; i < maxPendingRejections+10; i++ {
		s.add(Rejection{WorkspaceID: strconv.Itoa(i)}, 1)
	}
	if len(s.tally) != maxPendingRejections {
		t.Fatalf("tally holds %d keys, want %d", len(s.tally), maxPendingRejections)
	}
}
