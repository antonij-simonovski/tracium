// Package migrations applies immutable, namespaced Postgres migrations.
package migrations

import (
	"context"
	"crypto/sha256"
	"embed"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"io/fs"
	"path"
	"regexp"
	"sort"
)

//go:embed sql/*.sql
var coreFiles embed.FS
var Core = Set{Namespace: "core", Files: coreFiles, Dir: "sql"}

var namespacePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

type Set struct {
	Namespace string
	Files     fs.FS
	Dir       string
}

// Apply holds a transaction-scoped advisory lock across the entire set. DDL and
// history commit atomically. A checksum mismatch refuses an edited migration.
// This ledger is separate from the deployment's existing ClickHouse history.
func Apply(ctx context.Context, pool *pgxpool.Pool, set Set) error {
	if !namespacePattern.MatchString(set.Namespace) || set.Files == nil {
		return fmt.Errorf("invalid migration set %q", set.Namespace)
	}
	entries, err := fs.ReadDir(set.Files, set.Dir)
	if err != nil {
		return err
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && path.Ext(entry.Name()) == ".sql" {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	// One lock also serializes first-time ledger creation across namespaces.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(734162190284)`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS app_schema_migrations (
   namespace TEXT NOT NULL, filename TEXT NOT NULL, checksum TEXT NOT NULL,
   applied_at TIMESTAMPTZ NOT NULL DEFAULT now(), PRIMARY KEY(namespace,filename))`); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT filename,checksum FROM app_schema_migrations WHERE namespace=$1`, set.Namespace)
	if err != nil {
		return err
	}
	applied := map[string]string{}
	for rows.Next() {
		var name, sum string
		if err = rows.Scan(&name, &sum); err != nil {
			rows.Close()
			return err
		}
		applied[name] = sum
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	present := map[string]bool{}
	for _, name := range names {
		present[name] = true
	}
	for name := range applied {
		if !present[name] {
			return fmt.Errorf("missing applied migration %s/%s", set.Namespace, name)
		}
	}
	for _, name := range names {
		sql, err := fs.ReadFile(set.Files, path.Join(set.Dir, name))
		if err != nil {
			return err
		}
		checksum := fmt.Sprintf("%x", sha256.Sum256(sql))
		if old, ok := applied[name]; ok {
			if old != checksum {
				return fmt.Errorf("migration changed: %s/%s", set.Namespace, name)
			}
			continue
		}
		for old := range applied {
			if name < old {
				return fmt.Errorf("migration %s/%s precedes applied migration %s", set.Namespace, name, old)
			}
		}
		if _, err = tx.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("migration %s/%s: %w", set.Namespace, name, err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO app_schema_migrations(namespace,filename,checksum) VALUES($1,$2,$3)`, set.Namespace, name, checksum); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
