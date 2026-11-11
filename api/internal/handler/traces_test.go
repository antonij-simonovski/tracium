package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/tracium/api/testing/mocks"
)

func TestListTracesSilentFailureFilter(t *testing.T) {
	for value, want := range map[string]int{"truncated": 200, "refused": 200, "tool_error": 200, "any": 200, "error": 400} {
		repo := mocks.NewMockTraceRepository()
		rr := httptest.NewRecorder()
		serveAuthed(NewTraceHandler(repo, allowAllAccess{}).ListTraces, rr, httptest.NewRequest("GET", "/v1/traces?silent_failure="+value, nil))
		if rr.Code != want {
			t.Fatalf("%s: status %d, want %d: %s", value, rr.Code, want, rr.Body.String())
		}
		if want == 200 && repo.LastFilter.SilentFailure != value {
			t.Errorf("%s reached the repository as %q", value, repo.LastFilter.SilentFailure)
		}
	}
}
