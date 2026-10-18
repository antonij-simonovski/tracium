package traciumprocessor

import (
	"context"
	"testing"

	"go.opentelemetry.io/collector/client"
)

// fakeAuth is a client.AuthData carrying a fixed workspace, as the traciumauth
// extension would attach. workspace is `any` so tests can inject a wrong type.
type fakeAuth struct {
	workspace any
}

func (f fakeAuth) GetAttribute(name string) any {
	if name == authAttrWorkspace {
		return f.workspace
	}
	return nil
}
func (f fakeAuth) GetAttributeNames() []string { return []string{authAttrWorkspace} }

func ctxWithAuth(auth client.AuthData) context.Context {
	return client.NewContext(context.Background(), client.Info{Auth: auth})
}

func TestIngestScopeUnauthenticatedWithoutValidKey(t *testing.T) {
	if ingestScope(context.Background()).authenticated {
		t.Fatal("no auth on context must not be authenticated")
	}
	// Auth present but not from our extension (workspace attr wrong type).
	if ingestScope(ctxWithAuth(fakeAuth{workspace: 123})).authenticated {
		t.Fatal("non-extension auth must not count as authenticated")
	}
	// Auth present but workspace empty.
	if ingestScope(ctxWithAuth(fakeAuth{workspace: ""})).authenticated {
		t.Fatal("empty workspace must not count as authenticated")
	}
}

func TestIngestScopeReadsWorkspace(t *testing.T) {
	scope := ingestScope(ctxWithAuth(fakeAuth{workspace: "ws-1"}))
	if !scope.authenticated || scope.workspace != "ws-1" {
		t.Fatalf("scope = %+v, want authenticated+ws-1", scope)
	}
}
