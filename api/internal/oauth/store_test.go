package oauth_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/tracium/api/internal/oauth"
	"github.com/tracium/api/testing/pgtest"
)

func TestPostgresStore(t *testing.T) {
	ctx := t.Context()
	store := oauth.NewStore(pgtest.NewPool(t))
	c := oauth.Client{ID: uuid.NewString(), Name: "Claude", RedirectURIs: []string{"https://claude.ai/cb", "http://localhost/cb"}}
	if _, err := store.InsertClient(ctx, c); err != nil {
		t.Fatal(err)
	}
	got, err := store.Client(ctx, c.ID)
	if err != nil || got.Name != "Claude" || len(got.RedirectURIs) != 2 {
		t.Fatalf("client: %+v %v", got, err)
	}
	if _, err := store.Client(ctx, "not-a-uuid"); !errors.Is(err, oauth.ErrNotFound) {
		t.Fatalf("unknown client: %v", err)
	}

	code := oauth.Code{ClientID: c.ID, UserID: uuid.NewString(), RedirectURI: "https://claude.ai/cb", Challenge: "ch"}
	if err := store.InsertCode(ctx, "live", code, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertCode(ctx, "stale", code, -time.Second); err != nil {
		t.Fatal(err)
	}
	if got, err := store.ConsumeCode(ctx, "live"); err != nil || *got != code {
		t.Fatalf("consume: %+v %v", got, err)
	}
	for _, hash := range []string{"live", "stale", "unknown"} {
		if _, err := store.ConsumeCode(ctx, hash); !errors.Is(err, oauth.ErrNotFound) {
			t.Errorf("%s: %v", hash, err)
		}
	}
}
