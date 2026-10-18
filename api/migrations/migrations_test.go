package migrations

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"testing/fstest"
)

func TestUpgradeAndImmutableNamespaces(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	// Dedicated test DB only: the test never touches the configured application DSN.
	_, err = pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;
 CREATE TABLE workspaces(id TEXT PRIMARY KEY,user_id TEXT NOT NULL,name TEXT NOT NULL,slug TEXT NOT NULL,env TEXT NOT NULL,role TEXT NOT NULL,members INT NOT NULL DEFAULT 1,created_at TIMESTAMPTZ NOT NULL DEFAULT now());
 INSERT INTO workspaces(id,user_id,name,slug,env,role) VALUES('legacy-workspace','legacy-owner','Legacy','legacy','production','Owner')`)
	if err != nil {
		t.Fatal(err)
	}
	if err = Apply(ctx, pool, Core); err != nil {
		t.Fatal(err)
	}
	var owner string
	if err = pool.QueryRow(ctx, `SELECT user_id FROM workspace_members WHERE workspace_id='legacy-workspace' AND role='owner'`).Scan(&owner); err != nil || owner != "legacy-owner" {
		t.Fatalf("legacy membership lost: %s %v", owner, err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO workspaces(id,user_id,name,slug,env) VALUES('new-workspace','new-owner','New','new','production')`); err != nil {
		t.Fatalf("insert without role/members: %v", err)
	}
	var role string
	var members int
	if err = pool.QueryRow(ctx, `SELECT role,members FROM workspaces WHERE id='new-workspace'`).Scan(&role, &members); err != nil || role != "owner" || members != 1 {
		t.Fatalf("workspaces defaults: %q %d %v", role, members, err)
	}
	if err = Apply(ctx, pool, Core); err != nil {
		t.Fatal(err)
	}
	set := Set{Namespace: "test_extension", Dir: ".", Files: fstest.MapFS{"001.sql": {Data: []byte("CREATE TABLE extension_marker(id INT);")}}}
	if err = Apply(ctx, pool, set); err != nil {
		t.Fatal(err)
	}
	set.Files = fstest.MapFS{"001.sql": {Data: []byte("CREATE TABLE extension_marker(id TEXT);")}}
	if err = Apply(ctx, pool, set); err == nil {
		t.Fatal("edited migration was accepted")
	}
	set.Files = fstest.MapFS{}
	if err = Apply(ctx, pool, set); err == nil {
		t.Fatal("deleted migration was accepted")
	}
	failed := Set{Namespace: "failed_extension", Dir: ".", Files: fstest.MapFS{"001.sql": {Data: []byte("CREATE TABLE should_rollback(id INT); SELECT missing_column;")}}}
	if err = Apply(ctx, pool, failed); err == nil {
		t.Fatal("invalid migration succeeded")
	}
	var exists bool
	if err = pool.QueryRow(ctx, `SELECT to_regclass('should_rollback') IS NOT NULL`).Scan(&exists); err != nil || exists {
		t.Fatalf("failed DDL not rolled back: %v %v", exists, err)
	}

	// Deleting a workspace must invalidate its API keys. api_keys.workspace_id is
	// an ON DELETE CASCADE foreign key, so a key cannot outlive its workspace and
	// keep authenticating telemetry into a workspace that no longer exists.
	if _, err = pool.Exec(ctx,
		`INSERT INTO api_keys(id,workspace_id,created_by,name,prefix,key_hash)
		 VALUES(gen_random_uuid(),'legacy-workspace','legacy-owner','k','tk_ab','hash-cascade')`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM workspaces WHERE id='legacy-workspace'`); err != nil {
		t.Fatal(err)
	}
	var keyCount int
	if err = pool.QueryRow(ctx,
		`SELECT count(*) FROM api_keys WHERE workspace_id='legacy-workspace'`).Scan(&keyCount); err != nil {
		t.Fatal(err)
	}
	if keyCount != 0 {
		t.Fatalf("api keys survived workspace deletion: %d left", keyCount)
	}
}
