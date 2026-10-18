package mocks

import (
	"context"
	"sync"

	"github.com/tracium/collector/internal/deadletter"
)

// MockWriter records rejection counts. It satisfies deadletter.TallyWriter.
type MockWriter struct {
	mu       sync.Mutex
	Rejected map[deadletter.Rejection]uint64
}

// WriteRejected adds tally to Rejected.
func (m *MockWriter) WriteRejected(_ context.Context, tally map[deadletter.Rejection]uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Rejected == nil {
		m.Rejected = map[deadletter.Rejection]uint64{}
	}
	for r, n := range tally {
		m.Rejected[r] += n
	}
	return nil
}

// Close is a no-op.
func (m *MockWriter) Close() error { return nil }

// MockDeadLetterStore records every dead-lettered span. It satisfies
// deadletter.Store.
type MockDeadLetterStore struct {
	mu      sync.Mutex
	Records []deadletter.Record
	Closed  bool
}

// Put appends rec to Records.
func (m *MockDeadLetterStore) Put(_ context.Context, rec deadletter.Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Records = append(m.Records, rec)
	return nil
}

// Close marks the store closed.
func (m *MockDeadLetterStore) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Closed = true
	return nil
}

// Count returns how many records were dead-lettered.
func (m *MockDeadLetterStore) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Records)
}
