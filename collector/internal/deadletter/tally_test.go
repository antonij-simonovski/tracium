package deadletter_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/tracium/collector/internal/deadletter"
	"github.com/tracium/collector/pkg/spanmodel"
	"github.com/tracium/collector/testing/mocks"
)

func TestTallyStoreCountsPerWorkspaceAndForwards(t *testing.T) {
	next := &mocks.MockDeadLetterStore{}
	w := &mocks.MockWriter{}
	s := deadletter.NewTallyStore(next, w, slog.Default(), time.Hour)

	put := func(workspace, code string) {
		rec := deadletter.Record{Code: code, Span: &spanmodel.Span{WorkspaceID: workspace}}
		if err := s.Put(context.Background(), rec); err != nil {
			t.Fatal(err)
		}
	}
	put("ws-1", "invalid_timestamp")
	put("ws-1", "invalid_timestamp")
	put("ws-2", "field_too_long")
	put("", "unauthenticated")

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if next.Count() != 4 || !next.Closed {
		t.Fatalf("next got %d records (closed=%v), want all 4 forwarded and closed", next.Count(), next.Closed)
	}
	want := map[deadletter.Rejection]uint64{
		{WorkspaceID: "ws-1", Code: "invalid_timestamp"}: 2,
		{WorkspaceID: "ws-2", Code: "field_too_long"}:    1,
	}
	if len(w.Rejected) != len(want) {
		t.Fatalf("tally = %v, want %v", w.Rejected, want)
	}
	for r, n := range want {
		if w.Rejected[r] != n {
			t.Fatalf("tally[%v] = %d, want %d", r, w.Rejected[r], n)
		}
	}
}
