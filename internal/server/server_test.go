//go:build sqlite_fts5

package server_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/cgould/dtree/internal/config"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/identity"
	"github.com/cgould/dtree/internal/server"
	"github.com/cgould/dtree/internal/storage"
)

// ---- test helpers ----

func tempRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".decisions"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeActors(t *testing.T, repoRoot string, actors []core.Actor) {
	t.Helper()
	af := &storage.ActorsFile{Actors: actors}
	path := filepath.Join(repoRoot, ".decisions", storage.ActorsFileName)
	if err := storage.WriteActors(path, af); err != nil {
		t.Fatalf("writeActors: %v", err)
	}
}

func testResolver(t *testing.T, actors []core.Actor) (*identity.Resolver, string) {
	t.Helper()
	repo := tempRepo(t)
	writeActors(t, repo, actors)
	cfg := &config.Resolved{IdentitySrc: config.SourceDefault}
	r := identity.NewResolver(repo, cfg)
	return r, repo
}

func testConfig(t *testing.T) server.Config {
	t.Helper()
	resolver, repo := testResolver(t, []core.Actor{
		{Handle: "cam", Name: "Cameron", Kind: core.ActorHuman, Active: true},
	})
	return server.Config{
		Listen:   ":0",
		RepoRoot: repo,
		Resolver: resolver,
		Trust:    server.TrustLocalhostOnly,
	}
}

// ---- tests ----

// TestNewListenAddrAssigned verifies that Listen=":0" causes the OS to assign a port.
func TestNewListenAddrAssigned(t *testing.T) {
	cfg := testConfig(t)
	cfg.Listen = ":0"

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().String()
	if addr == "" || addr == ":0" {
		t.Errorf("expected a non-zero assigned address, got %q", addr)
	}

	// Build the server and verify it starts.
	srv := server.New(cfg)
	if srv == nil {
		t.Fatal("server.New returned nil")
	}

	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Shutdown(context.Background()) }()

	resp, err := http.Get("http://" + addr + "/v1/health")
	if err != nil {
		t.Fatalf("GET /v1/health: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d; want 200", resp.StatusCode)
	}
}

// TestHealthOK verifies that GET /v1/health returns 200 with ok=true.
func TestHealthOK(t *testing.T) {
	cfg := testConfig(t)
	srv := server.New(cfg)

	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d; want 200", w.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	ok, _ := body["ok"].(bool)
	if !ok {
		t.Errorf("body.ok = %v; want true", body["ok"])
	}
}

// TestHealthNoIdentityRequired verifies that /v1/health works without X-Dtree-As.
func TestHealthNoIdentityRequired(t *testing.T) {
	cfg := testConfig(t)
	srv := server.New(cfg)

	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	// Deliberately no X-Dtree-As header.
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d; want 200 (no identity should not block health)", w.Code)
	}
}

// TestUnknownEndpointReturnsProblem verifies that 404s use Problem Details JSON.
func TestUnknownEndpointReturnsProblem(t *testing.T) {
	cfg := testConfig(t)
	srv := server.New(cfg)

	req := httptest.NewRequest(http.MethodGet, "/v1/nope", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d; want 404", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "application/problem+json" {
		t.Errorf("Content-Type = %q; want application/problem+json", ct)
	}

	var p server.Problem
	if err := json.NewDecoder(w.Body).Decode(&p); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if p.Status != http.StatusNotFound {
		t.Errorf("problem.status = %d; want 404", p.Status)
	}
}

// TestIdentityHeaderInjectedIntoContext registers a test handler that reads
// IdentityFromContext, then GET with X-Dtree-As=cam expects handle=="cam".
func TestIdentityHeaderInjectedIntoContext(t *testing.T) {
	cfg := testConfig(t)
	srv := server.New(cfg)

	// Register a test handler on the chi router. We reach into the handler
	// which is a *chi.Mux, so we cast to chi.Router.
	mux, ok := srv.Handler.(chi.Router)
	if !ok {
		t.Fatal("server handler is not a chi.Router")
	}
	mux.Get("/v1/test", func(w http.ResponseWriter, r *http.Request) {
		handle, ok := server.IdentityFromContext(r.Context())
		if !ok {
			http.Error(w, "no identity", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(handle))
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/test", nil)
	req.Header.Set("X-Dtree-As", "cam")
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d; want 200", w.Code)
	}
	if got := w.Body.String(); got != "cam" {
		t.Errorf("body = %q; want %q", got, "cam")
	}
}

// TestIdentityHeaderMissing: GET /v1/test without X-Dtree-As, handler calls
// MustHaveIdentity → 401 Problem.
func TestIdentityHeaderMissing(t *testing.T) {
	cfg := testConfig(t)
	srv := server.New(cfg)

	mux, ok := srv.Handler.(chi.Router)
	if !ok {
		t.Fatal("server handler is not a chi.Router")
	}
	mux.Get("/v1/test", func(w http.ResponseWriter, r *http.Request) {
		if !server.MustHaveIdentity(w, r) {
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/test", nil)
	// No X-Dtree-As header.
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "application/problem+json" {
		t.Errorf("Content-Type = %q; want application/problem+json", ct)
	}
}

// TestUnknownIdentityReturns403: X-Dtree-As=ghost (not registered) → 403 Problem.
func TestUnknownIdentityReturns403(t *testing.T) {
	cfg := testConfig(t)
	srv := server.New(cfg)

	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	req.Header.Set("X-Dtree-As", "ghost")
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d; want 403", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "application/problem+json" {
		t.Errorf("Content-Type = %q; want application/problem+json", ct)
	}

	var p server.Problem
	if err := json.NewDecoder(w.Body).Decode(&p); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if p.Status != http.StatusForbidden {
		t.Errorf("problem.status = %d; want 403", p.Status)
	}
}

// TestProblemDetailsShape verifies that error responses have type/title/status/instance.
func TestProblemDetailsShape(t *testing.T) {
	cfg := testConfig(t)
	srv := server.New(cfg)

	req := httptest.NewRequest(http.MethodGet, "/v1/nope", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	for _, field := range []string{"type", "title", "status", "instance"} {
		if _, exists := body[field]; !exists {
			t.Errorf("problem body missing field %q", field)
		}
	}

	if instance, _ := body["instance"].(string); instance != "/v1/nope" {
		t.Errorf("instance = %q; want %q", instance, "/v1/nope")
	}
}

// TestRecoveryMiddleware: panic in a handler → 500 Problem, server stays up.
func TestRecoveryMiddleware(t *testing.T) {
	cfg := testConfig(t)
	srv := server.New(cfg)

	mux, ok := srv.Handler.(chi.Router)
	if !ok {
		t.Fatal("server handler is not a chi.Router")
	}
	mux.Get("/v1/panic", func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/panic", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d; want 500", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "application/problem+json" {
		t.Errorf("Content-Type = %q; want application/problem+json", ct)
	}

	// Server is still alive — another request should work.
	req2 := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	w2 := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Errorf("after-panic health status = %d; want 200", w2.Code)
	}
}
