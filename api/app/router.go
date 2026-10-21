package app

import (
	"github.com/go-chi/chi/v5"
	"github.com/tracium/api/extension"
	"github.com/tracium/api/internal/apikey"
	"github.com/tracium/api/internal/auth"
	"github.com/tracium/api/internal/handler"
	"github.com/tracium/api/internal/middleware"
	"github.com/tracium/api/internal/model"
	"github.com/tracium/api/internal/query"
	"github.com/tracium/api/internal/version"
	"github.com/tracium/api/internal/workspace"
	"log"
	"net/http"
	"time"
)

func newRouter(cfg Config, repo query.Repository, wsStore workspace.Store, inviteStore workspace.InviteStore, userStore *auth.UserStore, authenticator middleware.Authenticator, authService *auth.Service, apiKeyService *apikey.Service, healthChecks []handler.DependencyCheck, opts Options) http.Handler {
	authHandler := handler.NewAuthHandler(authService)
	workspaceHandler := handler.NewWorkspaceHandler(wsStore, userStore, opts.Entitlements)
	apiKeyHandler := handler.NewAPIKeyHandler(apiKeyService, wsStore)
	inviteHandler := handler.NewInviteHandler(inviteStore, wsStore, opts.Entitlements, opts.Invites)
	services := extension.Services{Mail: opts.Mail, Entitlements: opts.Entitlements, Workspaces: wsStore}

	traceHandler := handler.NewTraceHandler(repo, wsStore)
	spanHandler := handler.NewSpanHandler(repo, wsStore)
	metricsHandler := handler.NewMetricsHandler(repo, wsStore)
	queryHandler := handler.NewQueryHandler(repo, repo, wsStore)
	healthHandler := handler.NewHealthHandler(healthChecks...)

	r := chi.NewRouter()

	// Global middleware chain — order matters; do not reorder.
	r.Use(middleware.CORS())
	r.Use(middleware.APIVersion(version.V1))

	// Health and readiness probes are unauthenticated.
	r.Get(version.Route(version.V1, "/health"), healthHandler.Health)
	r.Get(version.Route(version.V1, "/ready"), healthHandler.Ready)

	// Account registration and login are unauthenticated, so they are the most
	// exposed surface — throttle them per client IP to blunt brute-force and
	// enumeration. A negative limit disables it (see AuthConfig.RateLimitPerMinute).
	r.Group(func(r chi.Router) {
		if cfg.Auth.RateLimitPerMinute >= 0 {
			limiter := middleware.NewRateLimiter(cfg.Auth.RateLimitPerMinute, time.Minute, cfg.Auth.TrustedProxies)
			r.Use(limiter.Middleware())
			log.Printf("auth endpoints throttled to %d requests/min per client IP", cfg.Auth.RateLimitPerMinute)
		} else {
			log.Println("WARNING: auth endpoint rate limiting is disabled")
		}
		r.Post(version.Route(version.V1, "/auth/register"), authHandler.Register)
		r.Post(version.Route(version.V1, "/auth/login"), authHandler.Login)
		// Session-less, so it shares the auth limiter.
		r.Get(version.Route(version.V1, "/invites/{token}"), inviteHandler.Preview)
	})

	// Ingest key verification is called by the collector's authenticator, not a
	// logged-in user, so it carries no bearer token. It is still an online-guessing
	// surface, so it is throttled per client IP — but with its OWN limiter, not the
	// login bucket. The caller is a collector: one IP fronting many senders and
	// machine traffic, so it needs a larger, dedicated allowance. Sharing login's
	// tiny bucket let a handful of invalid keys exhaust the collector's budget and
	// block verification of legitimate keys.
	r.Group(func(r chi.Router) {
		if cfg.Auth.VerifyRateLimitPerMinute >= 0 {
			limiter := middleware.NewRateLimiter(cfg.Auth.VerifyRateLimitPerMinute, time.Minute, cfg.Auth.TrustedProxies)
			r.Use(limiter.Middleware())
			log.Printf("ingest verify endpoint throttled to %d requests/min per client IP", cfg.Auth.VerifyRateLimitPerMinute)
		} else {
			log.Println("WARNING: ingest verify endpoint rate limiting is disabled")
		}
		r.Post(version.Route(version.V1, "/ingest/keys/verify"), apiKeyHandler.Verify)
	})

	// Account and workspace routes.
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(authenticator))
		// Self-service password change for the signed-in user. Unlike register/login
		// this is authenticated, so it doesn't need its own rate limiter — the auth
		// middleware already requires a valid session.
		r.Post(version.Route(version.V1, "/auth/password"), authHandler.ChangePassword)
		r.Get(version.Route(version.V1, "/workspaces"), workspaceHandler.List)
		// Workspace creation passes through the entitlements provider; the default
		// provider allows it.
		r.With(services.Gate("workspaces.create")).Post(version.Route(version.V1, "/workspaces"), workspaceHandler.Create)
		r.Delete(version.Route(version.V1, "/workspaces/{id}"), workspaceHandler.Delete)
		r.Post(version.Route(version.V1, "/workspaces/{id}/members"), workspaceHandler.AddMember)
		r.Delete(version.Route(version.V1, "/workspaces/{id}/members/{userId}"), workspaceHandler.RemoveMember)
		r.Get(version.Route(version.V1, "/workspaces/{id}/members"), workspaceHandler.ListMembers)

		r.Get(version.Route(version.V1, "/workspaces/{id}/invites"), inviteHandler.List)
		r.Post(version.Route(version.V1, "/workspaces/{id}/invites"), inviteHandler.Create)
		r.Delete(version.Route(version.V1, "/workspaces/{id}/invites/{inviteId}"), inviteHandler.Revoke)
		r.Post(version.Route(version.V1, "/invites/{token}/accept"), inviteHandler.Accept)

		// Ingest key management — a key is bound to one workspace, so the routes
		// nest under it and are gated on membership. The secret is returned only
		// from Create.
		r.Get(version.Route(version.V1, "/workspaces/{id}/api-keys"), apiKeyHandler.List)
		r.Post(version.Route(version.V1, "/workspaces/{id}/api-keys"), apiKeyHandler.Create)
		r.Delete(version.Route(version.V1, "/workspaces/{id}/api-keys/{keyId}"), apiKeyHandler.Revoke)

		// Read keys belong to the signed-in account, not a workspace.
		r.Get(version.Route(version.V1, "/account/api-keys"), apiKeyHandler.ListRead)
		r.Post(version.Route(version.V1, "/account/api-keys"), apiKeyHandler.CreateRead)
		r.Delete(version.Route(version.V1, "/account/api-keys/{keyId}"), apiKeyHandler.RevokeRead)
	})

	for _, ext := range opts.Extensions {
		ext := ext
		if ext.Routes != nil {
			r.Route("/v1/extensions/"+ext.Name, func(r chi.Router) {
				r.Use(middleware.Auth(authenticator))
				ext.Routes(r, services)
			})
		}
		if ext.Webhooks != nil {
			r.Mount("/v1/integrations/"+ext.Name, ext.Webhooks)
		}
	}

	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(authenticator))
		// Telemetry reads pass through the entitlements provider for the caller;
		// the default provider allows them.
		r.Use(services.Gate("telemetry.read"))

		r.Get(version.Route(version.V1, "/traces"), traceHandler.ListTraces)
		r.Get(version.Route(version.V1, "/traces/{id}"), traceHandler.GetTrace)
		r.Get(version.Route(version.V1, "/traces/{traceId}/spans"), spanHandler.ListSpans)

		r.Get(version.Route(version.V1, "/metrics/kpis"), metricsHandler.KPIs)
		r.Get(version.Route(version.V1, "/metrics/cost-series"), metricsHandler.CostSeries)
		r.Get(version.Route(version.V1, "/metrics/latency-series"), metricsHandler.LatencySeries)
		r.Get(version.Route(version.V1, "/metrics/error-series"), metricsHandler.ErrorSeries)
		r.Get(version.Route(version.V1, "/metrics/token-series"), metricsHandler.TokenSeries)
		r.Get(version.Route(version.V1, "/metrics/top-workflows"), metricsHandler.TopWorkflows)
		r.Get(version.Route(version.V1, "/metrics/workflows"), metricsHandler.Workflows)
		r.Get(version.Route(version.V1, "/metrics/workflows/{name}"), metricsHandler.WorkflowDetail)
		r.Get(version.Route(version.V1, "/metrics/workflows/{name}/versions"), metricsHandler.WorkflowVersions)
		r.Get(version.Route(version.V1, "/metrics/failures"), metricsHandler.Failures)
		r.Get(version.Route(version.V1, "/metrics/model-costs"), metricsHandler.ModelCosts)
		r.Get(version.Route(version.V1, "/metrics/usage-users"), metricsHandler.UserUsage)
		r.Get(version.Route(version.V1, "/metrics/usage-workflows"), metricsHandler.WorkflowUsage)
		r.Get(version.Route(version.V1, "/metrics/attribute-keys"), metricsHandler.AttributeKeys)
		r.Get(version.Route(version.V1, "/metrics/usage-by-attribute"), metricsHandler.UsageByAttribute)
		r.Get(version.Route(version.V1, "/metrics/anomalies"), metricsHandler.Anomalies)
	})

	// One bucket per account for the query API and both MCP routes: read keys
	// hand them to machine clients, and each query can scan up to 92 days of
	// spans. Hosted connectors share egress IPs, so the key is the account.
	queryLimit := func(next http.Handler) http.Handler { return next }
	if cfg.Auth.QueryRateLimitPerMinute >= 0 {
		queryLimit = middleware.NewRateLimiter(cfg.Auth.QueryRateLimitPerMinute, time.Minute, cfg.Auth.TrustedProxies).PerPrincipal()
		log.Printf("query and MCP endpoints throttled to %d requests/min per account", cfg.Auth.QueryRateLimitPerMinute)
	} else {
		log.Println("WARNING: query and MCP endpoint rate limiting is disabled")
	}

	// The query API and the MCP server also accept read keys, which act as their
	// owner since MCP clients outlive sessions. Ingest keys never read here.
	r.Group(func(r chi.Router) {
		r.Use(middleware.APIKeyOr(apiKeyService, middleware.Auth(authenticator), model.ScopeRead))
		r.Use(queryLimit)
		r.Use(services.Gate("telemetry.read"))
		r.Post(version.Route(version.V1, "/query"), queryHandler.Query)
		r.Get(version.Route(version.V1, "/query/schema"), queryHandler.Schema)
		r.Post(version.Route(version.V1, "/mcp"), queryHandler.MCP)
	})

	// MCP connectors that take only a URL carry the read key as its last segment.
	r.With(middleware.BearerFromPath("key"), middleware.APIKeyOr(apiKeyService, middleware.Auth(authenticator), model.ScopeRead), queryLimit, services.Gate("telemetry.read")).
		Post(version.Route(version.V1, "/mcp/{key}"), queryHandler.MCP)

	// Setup checks also accept the workspace's api keys, so a coding agent can
	// verify instrumentation without a dashboard login.
	r.With(middleware.APIKeyOr(apiKeyService, middleware.Auth(authenticator), model.ScopeIngest, model.ScopeRead), services.Gate("telemetry.read")).
		Get(version.Route(version.V1, "/metrics/setup-checks"), metricsHandler.SetupChecks)

	return r
}
