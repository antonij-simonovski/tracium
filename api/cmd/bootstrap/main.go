// Command bootstrap gives a fresh local install an ingest key without the
// dashboard: it creates the account if missing, a workspace if the account owns
// none, and a new API key, then prints the key (trc_...) to stdout. Like
// reset-password it is an admin operation for whoever can reach the API's
// database, e.g. `docker compose exec api ./bootstrap --email you@example.com`,
// and is not exposed over HTTP.
//
// Usage:
//
//	bootstrap --email user@example.com [--password PASS] [--workspace NAME]
//
// A new account gets --password, or a generated one printed to stderr. An
// existing account keeps its password, and its first workspace is reused.
//
// Connection: POSTGRES_DSN (same as the API).
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"github.com/tracium/api/internal/apikey"
	"github.com/tracium/api/internal/auth"
	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/internal/workspace"
)

func main() {
	_ = godotenv.Load()

	var (
		email    = flag.String("email", "", "account to bootstrap (required)")
		password = flag.String("password", "", "password for a new account (default: generated)")
		wsName   = flag.String("workspace", "Default", "name of the workspace created when the account owns none")
	)
	flag.Parse()

	if *email == "" {
		fmt.Fprintln(os.Stderr, "✘ --email is required")
		flag.Usage()
		os.Exit(2)
	}

	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		fail(errors.New("POSTGRES_DSN is not set"))
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fail(err)
	}
	defer pool.Close()

	res, err := bootstrap(ctx, pool, *email, *password, *wsName)
	if err != nil {
		fail(err)
	}
	if res.GeneratedPassword != "" {
		fmt.Fprintf(os.Stderr, "✔ created account %s\n  password: %s\n", res.Email, res.GeneratedPassword)
	}
	fmt.Fprintf(os.Stderr, "✔ api key %q for workspace %q\n", res.KeyName, res.Workspace)
	fmt.Println(res.Token)
}

type result struct {
	Email             string
	GeneratedPassword string
	Workspace         string
	KeyName           string
	Token             string
}

func bootstrap(ctx context.Context, pool *pgxpool.Pool, email, password, wsName string) (result, error) {
	users := auth.NewUserStore(pool)
	email = auth.NormalizeEmail(email)
	res := result{Email: email, KeyName: "agent-setup"}

	user, err := users.ByEmail(ctx, email)
	if errors.Is(err, auth.ErrUserNotFound) {
		if password == "" {
			if password, err = randomPassword(); err != nil {
				return result{}, err
			}
			res.GeneratedPassword = password
		}
		if err := auth.ValidateCredentials(email, password); err != nil {
			return result{}, err
		}
		// The session token Register signs is discarded, so the issuer's secret is irrelevant.
		if _, err := auth.NewService(users, auth.NewTokenIssuer(""), nil).Register(ctx, email, password); err != nil {
			return result{}, err
		}
		if user, err = users.ByEmail(ctx, email); err != nil {
			return result{}, err
		}
	} else if err != nil {
		return result{}, fmt.Errorf("look up %s: %w", email, err)
	}

	workspaces := workspace.NewStore(pool)
	existing, err := workspaces.List(ctx, user.ID)
	if err != nil {
		return result{}, err
	}
	i := slices.IndexFunc(existing, func(w model.Workspace) bool { return w.Role == workspace.RoleOwner })
	var ws model.Workspace
	if i >= 0 {
		ws = existing[i]
	} else {
		ws = model.Workspace{ID: uuid.NewString(), UserID: user.ID, Name: wsName, Slug: slug(wsName), Env: "development"}
		if err := workspaces.Create(ctx, ws); err != nil {
			return result{}, err
		}
	}
	res.Workspace = ws.Name

	created, err := apikey.NewService(apikey.NewStore(pool)).Create(ctx, ws.ID, user.ID, res.KeyName)
	if err != nil {
		return result{}, err
	}
	res.Token = created.Token
	return res, nil
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slug(name string) string {
	if s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(name), "-"), "-"); s != "" {
		return s
	}
	return "default"
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
