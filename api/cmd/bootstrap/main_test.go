package main

import (
	"context"
	"testing"

	"github.com/tracium/api/internal/apikey"
	"github.com/tracium/api/internal/auth"
	"github.com/tracium/api/internal/workspace"
	"github.com/tracium/api/testing/pgtest"
)

func TestBootstrap(t *testing.T) {
	pool := pgtest.NewPool(t)
	ctx := context.Background()
	keys := apikey.NewService(apikey.NewStore(pool))

	first, err := bootstrap(ctx, pool, " Agent@Example.com ", "", "My Team")
	if err != nil {
		t.Fatal(err)
	}
	if first.GeneratedPassword == "" || first.Email != "agent@example.com" || first.Workspace != "My Team" {
		t.Fatalf("first run: %+v", first)
	}
	user, err := auth.NewUserStore(pool).ByEmail(ctx, "agent@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.NewService(auth.NewUserStore(pool), auth.NewTokenIssuer("s"), nil).Login(ctx, "agent@example.com", first.GeneratedPassword); err != nil {
		t.Fatalf("generated password does not log in: %v", err)
	}
	wsID, err := keys.Verify(ctx, first.Token)
	if err != nil {
		t.Fatal(err)
	}

	second, err := bootstrap(ctx, pool, "agent@example.com", "", "Other")
	if err != nil {
		t.Fatal(err)
	}
	if second.GeneratedPassword != "" || second.Workspace != "My Team" || second.Token == first.Token {
		t.Fatalf("second run: %+v", second)
	}
	if again, _ := keys.Verify(ctx, second.Token); again != wsID {
		t.Fatalf("second key in workspace %q, want %q", again, wsID)
	}
	all, err := workspace.NewStore(pool).List(ctx, user.ID)
	if err != nil || len(all) != 1 || all[0].Slug != "my-team" || all[0].Role != workspace.RoleOwner {
		t.Fatalf("workspaces %+v, %v", all, err)
	}

	if _, err := bootstrap(ctx, pool, "new@example.com", "short", "Default"); err == nil {
		t.Fatal("weak password accepted")
	}
}
