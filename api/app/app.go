// Package app assembles the shared API for standalone and extended applications.
package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tracium/api/extension"
	"github.com/tracium/api/internal/apikey"
	"github.com/tracium/api/internal/auth"
	"github.com/tracium/api/internal/config"
	"github.com/tracium/api/internal/handler"
	"github.com/tracium/api/internal/query"
	"github.com/tracium/api/internal/workspace"
	"github.com/tracium/api/migrations"
	"net/http"
	"os"
	"regexp"
	"time"
)

type Config = config.Config
type ServerConfig = config.ServerConfig
type StorageConfig = config.StorageConfig
type AuthConfig = config.AuthConfig

// Extension routes always inherit authentication. Webhooks have a separate,
// explicitly public namespace and must verify their own provider signatures.
type Extension struct {
	Name     string
	Routes   func(chi.Router, extension.Services)
	Webhooks http.Handler
}
type Options struct {
	Extensions   []Extension
	Mail         extension.Mailer
	Entitlements extension.Entitlements
	Migrations   []migrations.Set
	// Accounts is an optional account lifecycle hook. When set (as the hosted
	// service does), registration requires email confirmation and login is gated
	// on it. When nil, registration and login run without a verification step.
	Accounts extension.AccountLifecycle
	// Invites is an optional hook called after a workspace invite is created, so
	// the hosted service can email the link. When nil, no notification is sent.
	Invites extension.InviteNotifier
}
type Application struct {
	Handler  http.Handler
	cfg      Config
	sessions extension.Sessions
	close    func()
}

func LoadConfig() (Config, error) {
	cfg, err := config.Load(os.Getenv("CONFIG_FILE"))
	if err != nil {
		return Config{}, err
	}
	if err := cfg.ApplyEnv(); err != nil {
		return Config{}, err
	}
	return *cfg, cfg.Validate()
}

func New(ctx context.Context, cfg Config, opts Options) (*Application, error) {
	cfg.Default()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if err := validateOptions(opts); err != nil {
		return nil, err
	}
	if opts.Mail == nil {
		opts.Mail = extension.DisabledMailer{}
	}
	if opts.Entitlements == nil {
		opts.Entitlements = extension.CoreEntitlements{}
	}
	pool, err := pgxpool.New(ctx, cfg.Storage.PostgresDSN)
	if err != nil {
		return nil, err
	}
	for _, set := range append([]migrations.Set{migrations.Core}, opts.Migrations...) {
		if err := migrations.Apply(ctx, pool, set); err != nil {
			pool.Close()
			return nil, err
		}
	}
	repo, err := query.NewClickHouseRepository(cfg.Storage.ClickHouseDSN, time.Duration(cfg.Storage.QueryTimeout)*time.Second)
	if err != nil {
		pool.Close()
		return nil, err
	}
	users := auth.NewUserStore(pool)
	workspaces := workspace.NewStore(pool)
	apiKeys := apikey.NewStore(pool)
	apiKeyService := apikey.NewService(apiKeys)
	service := auth.NewService(users, auth.NewTokenIssuer(cfg.Auth.JWTSecret), opts.Accounts)
	health := []handler.DependencyCheck{{Name: "clickhouse", Check: repo.Ping}, {Name: "postgres", Check: pool.Ping}}
	return &Application{cfg: cfg, sessions: service, Handler: newRouter(cfg, repo, workspaces, workspaces, users, service, service, apiKeyService, health, opts), close: func() { pool.Close(); repo.Close() }}, nil
}

var extensionName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func validateOptions(opts Options) error {
	names := map[string]bool{}
	for _, ext := range opts.Extensions {
		if !extensionName.MatchString(ext.Name) || names[ext.Name] {
			return fmt.Errorf("invalid or duplicate extension name %q", ext.Name)
		}
		names[ext.Name] = true
	}
	namespaces := map[string]bool{"core": true}
	for _, set := range opts.Migrations {
		if namespaces[set.Namespace] {
			return fmt.Errorf("duplicate migration namespace %q", set.Namespace)
		}
		namespaces[set.Namespace] = true
	}
	return nil
}

// Sessions lets an embedding application sign in an account it authenticated
// itself. Bind it to extension handlers after New; they serve no requests
// before Run.
func (a *Application) Sessions() extension.Sessions { return a.sessions }

func (a *Application) Close() {
	if a.close != nil {
		a.close()
		a.close = nil
	}
}

// Run serves until cancellation and drains requests before returning.
func (a *Application) Run(ctx context.Context) error {
	srv := &http.Server{Addr: a.cfg.Server.Addr, Handler: a.Handler,
		ReadTimeout:  time.Duration(a.cfg.Server.ReadTimeoutSeconds) * time.Second,
		WriteTimeout: time.Duration(a.cfg.Server.WriteTimeoutSeconds) * time.Second}
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		stop, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(stop); err != nil {
			_ = srv.Close()
			<-done
			return err
		}
		err := <-done
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
