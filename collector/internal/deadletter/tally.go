package deadletter

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Rejection is the key rejected spans are counted by.
type Rejection struct {
	WorkspaceID string
	Code        string
}

const (
	flushTimeout = 10 * time.Second
	// maxPendingRejections caps the distinct counts held between flushes, so a
	// long ClickHouse outage cannot grow the tally without bound.
	maxPendingRejections = 10_000
)

// TallyWriter persists rejection counts.
type TallyWriter interface {
	WriteRejected(ctx context.Context, tally map[Rejection]uint64) error
	Close() error
}

// TallyStore counts rejected spans per workspace and code, flushing the counts
// every interval, and forwards each record to the next store.
type TallyStore struct {
	next   Store
	writer TallyWriter
	logger Logger

	mu    sync.Mutex
	tally map[Rejection]uint64

	stop chan struct{}
	done chan struct{}
}

// NewTallyStore starts a TallyStore that flushes to w every interval.
func NewTallyStore(next Store, w TallyWriter, logger Logger, interval time.Duration) *TallyStore {
	s := &TallyStore{
		next:   next,
		writer: w,
		logger: logger,
		tally:  map[Rejection]uint64{},
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	go s.run(interval)
	return s
}

// Put implements Store.
func (s *TallyStore) Put(ctx context.Context, rec Record) error {
	// Keyless spans have no workspace and are not charged to any.
	if rec.Span != nil && rec.Span.WorkspaceID != "" {
		s.mu.Lock()
		s.add(Rejection{WorkspaceID: rec.Span.WorkspaceID, Code: rec.Code}, 1)
		s.mu.Unlock()
	}
	return s.next.Put(ctx, rec)
}

// Close implements Store, flushing pending counts first.
func (s *TallyStore) Close() error {
	close(s.stop)
	<-s.done
	return errors.Join(s.writer.Close(), s.next.Close())
}

func (s *TallyStore) run(interval time.Duration) {
	defer close(s.done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.flush()
		case <-s.stop:
			s.flush()
			return
		}
	}
}

func (s *TallyStore) flush() {
	s.mu.Lock()
	tally := s.tally
	if len(tally) == 0 {
		s.mu.Unlock()
		return
	}
	s.tally = map[Rejection]uint64{}
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
	defer cancel()
	if err := s.writer.WriteRejected(ctx, tally); err != nil {
		s.logger.Warn("rejected-span tally not written", "rejections", len(tally), "error", err)
		s.mu.Lock()
		for r, n := range tally {
			s.add(r, n)
		}
		s.mu.Unlock()
	}
}

// add must be called with s.mu held.
func (s *TallyStore) add(r Rejection, n uint64) {
	if _, ok := s.tally[r]; ok || len(s.tally) < maxPendingRejections {
		s.tally[r] += n
	}
}
