package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/cgould/dtree/internal/identity"
	"github.com/cgould/dtree/internal/index"
)

// ctxKeyIdentity is the typed context key for the resolved identity handle.
type ctxKeyIdentity struct{}

// IdentityFromContext retrieves the actor handle injected by the identity
// middleware. Returns ("", false) if no identity was set.
func IdentityFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(ctxKeyIdentity{}).(string)
	return v, ok && v != ""
}

// MustHaveIdentity is a helper for route handlers that require authentication.
// It writes a 401 Problem response and returns false if no identity is present.
func MustHaveIdentity(w http.ResponseWriter, r *http.Request) bool {
	if _, ok := IdentityFromContext(r.Context()); !ok {
		WriteProblem(w, r, Unauthorized("no identity provided"))
		return false
	}
	return true
}

// requestLogger returns a middleware that logs each request with slog.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		slog.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", middleware.GetReqID(r.Context()),
		)
	})
}

// recoverer returns a middleware that catches panics, logs them, and writes
// a 500 Problem Details response so the server stays alive.
func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				stack := debug.Stack()
				slog.Error("panic recovered",
					"panic", fmt.Sprintf("%v", rec),
					"stack", string(stack),
					"request_id", middleware.GetReqID(r.Context()),
				)
				WriteProblem(w, r, Internal("an unexpected error occurred"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// corsLocalhost adds permissive CORS headers for localhost origins only.
func corsLocalhost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			// Allow any localhost / loopback origin
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Dtree-As, If-Match")
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// identityFromHeader resolves the caller identity based on the configured
// Trust strategy and injects the handle into the request context.
//
// TrustLocalhostOnly: reads X-Dtree-As header, validates against resolver.
//   - header present + known actor → inject handle
//   - header present + unknown actor → 403
//   - header missing → continue without identity
//
// TrustToken: reads Authorization: Bearer <token>, validates via tokens table.
//   - valid token → inject handle from token record
//   - invalid/revoked/missing token → 401
func identityFromHeader(cfg Config, resolver *identity.Resolver, db *index.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch cfg.Trust {
			case TrustToken:
				authHeader := r.Header.Get("Authorization")
				if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
					WriteProblem(w, r, Unauthorized("missing or invalid Authorization header"))
					return
				}
				plaintext := strings.TrimPrefix(authHeader, "Bearer ")
				tok, err := index.LookupToken(db, plaintext)
				if err != nil {
					if errors.Is(err, sql.ErrNoRows) {
						WriteProblem(w, r, Unauthorized("invalid or expired token"))
						return
					}
					WriteProblem(w, r, Internal("token lookup failed"))
					return
				}
				ctx := context.WithValue(r.Context(), ctxKeyIdentity{}, tok.Handle)
				next.ServeHTTP(w, r.WithContext(ctx))

			default: // TrustLocalhostOnly
				handle := r.Header.Get("X-Dtree-As")
				if handle == "" {
					next.ServeHTTP(w, r)
					return
				}

				actor, err := resolver.FindActor(handle)
				if err != nil {
					WriteProblem(w, r, Internal("identity lookup failed"))
					return
				}
				if actor == nil {
					WriteProblem(w, r, Forbidden("actor not registered"))
					return
				}

				ctx := context.WithValue(r.Context(), ctxKeyIdentity{}, handle)
				next.ServeHTTP(w, r.WithContext(ctx))
			}
		})
	}
}
