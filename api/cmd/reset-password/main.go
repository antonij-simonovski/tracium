// Command reset-password sets a user's password directly, for self-hosted
// deployments that have no email/token reset flow. It is an admin operation:
// run it against the database the API server uses, e.g. on the host or in the
// container, not exposed over HTTP.
//
// Usage:
//
//	reset-password --email user@example.com [--password NEWPASS] [--yes]
//
//	--email PASSWORD   account to reset (required)
//	--password PASS    new password (default: a random one is generated and printed)
//	--yes              do not prompt for confirmation
//
// Connection: POSTGRES_DSN (same as the API). This tool only touches Postgres,
// so unlike the API server it does not require CLICKHOUSE_DSN or JWT_SECRET.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"github.com/tracium/api/internal/auth"
)

func main() {
	_ = godotenv.Load()

	var (
		email    = flag.String("email", "", "account to reset (required)")
		password = flag.String("password", "", "new password (default: generated)")
		yes      = flag.Bool("yes", false, "do not prompt for confirmation")
	)
	flag.Parse()

	if *email == "" {
		fmt.Fprintln(os.Stderr, "✘ --email is required")
		flag.Usage()
		os.Exit(2)
	}

	newPassword := *password
	generated := false
	if newPassword == "" {
		pw, err := randomPassword()
		if err != nil {
			fail(err)
		}
		newPassword = pw
		generated = true
	}

	if err := auth.ValidateCredentials(*email, newPassword); err != nil {
		fail(err)
	}

	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		fail(fmt.Errorf("POSTGRES_DSN is not set"))
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fail(err)
	}
	defer pool.Close()
	store := auth.NewUserStore(pool)

	user, err := store.ByEmail(ctx, auth.NormalizeEmail(*email))
	if err != nil {
		fail(fmt.Errorf("look up %s: %w", *email, err))
	}

	if !*yes {
		fmt.Printf("Reset password for %s? [y/N] ", user.Email)
		var reply string
		fmt.Scanln(&reply)
		if r := strings.ToLower(strings.TrimSpace(reply)); r != "y" && r != "yes" {
			fmt.Println("  aborted.")
			os.Exit(1)
		}
	}

	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		fail(err)
	}
	if err := store.UpdatePasswordHash(ctx, user.ID, hash); err != nil {
		fail(err)
	}

	fmt.Printf("✔ password reset for %s\n", user.Email)
	if generated {
		fmt.Printf("  new password: %s\n", newPassword)
	}
}

func randomPassword() (string, error) {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "✘ %v\n", err)
	os.Exit(1)
}
