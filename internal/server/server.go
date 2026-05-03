package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/cgould/dtree/internal/identity"
	"github.com/cgould/dtree/internal/index"
)

// Trust is the identity-trust strategy for the server.
type Trust int

const (
	// TrustLocalhostOnly requires X-Dtree-As and is intended for loopback use.
	TrustLocalhostOnly Trust = iota
	// TrustToken requires a Bearer token validated via the tokens table (future task).
	TrustToken
)

// Config holds the configuration for the HTTP server.
type Config struct {
	// Listen is the address to listen on, e.g. "127.0.0.1:8080" or ":0".
	Listen string

	// RepoRoot is the path to the repository root.
	RepoRoot string

	// DB is the open SQLite index.
	DB *index.DB

	// Resolver is the identity resolver for actor lookups.
	Resolver *identity.Resolver

	// ReadOnly causes mutation endpoints to be refused when true.
	ReadOnly bool

	// Trust is the identity-trust strategy.
	Trust Trust

	// Version is the application version string reported by /v1/health.
	// Defaults to "dev" if empty.
	Version string
}

// New constructs an *http.Server ready to call .Serve(listener) on.
// Middleware order: requestID → logger → recover → cors-localhost →
// identity-from-header → routes.
func New(cfg Config) *http.Server {
	if cfg.Version == "" {
		cfg.Version = "dev"
	}

	r := chi.NewRouter()

	// Core middleware stack.
	r.Use(chimiddleware.RequestID)
	r.Use(requestLogger)
	r.Use(recoverer)
	r.Use(corsLocalhost)
	r.Use(identityFromHeader(cfg.Resolver))

	// Mount /v1 routes.
	r.Route("/v1", func(r chi.Router) {
		r.Get("/health", healthHandler(cfg.Version))
		mountTrees(r, cfg)

		// Audit endpoints.
		ah := newAuditHandlers(cfg.RepoRoot)
		mountAuditRoutes(r, ah)
	})

	// Catch-all for unknown routes: return Problem Details 404.
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		WriteProblem(w, r, NotFound("the requested endpoint does not exist"))
	})

	return &http.Server{
		Addr:    cfg.Listen,
		Handler: r,
	}
}
