//go:build sqlite_fts5

package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/server"
)

// openTestIndex opens a fresh in-memory-ish SQLite index for tests.
func openTestIndex(t *testing.T) *index.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := index.Open(filepath.Join(dir, ".index.db"))
	if err != nil {
		t.Fatalf("open test index: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// testTokenConfig builds a server.Config with TrustToken strategy.
func testTokenConfig(t *testing.T, db *index.DB) server.Config {
	t.Helper()
	resolver, repo := testResolver(t, nil)
	return server.Config{
		Listen:   ":0",
		RepoRoot: repo,
		DB:       db,
		Resolver: resolver,
		Trust:    server.TrustToken,
	}
}

// TestTrustTokenHappy: valid bearer token → identity injected → handler returns 200.
func TestTrustTokenHappy(t *testing.T) {
	db := openTestIndex(t)
	cfg := testTokenConfig(t, db)
	srv := server.New(cfg)

	plaintext, err := index.CreateToken(db, "alice", "test-label", 0)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d; want 200", w.Code)
	}
}

// TestTrustTokenMissingBearer: no Authorization header → 401.
func TestTrustTokenMissingBearer(t *testing.T) {
	db := openTestIndex(t)
	cfg := testTokenConfig(t, db)
	srv := server.New(cfg)

	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	// No Authorization header.
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

// TestTrustTokenRevoked: revoked token → 401.
func TestTrustTokenRevoked(t *testing.T) {
	db := openTestIndex(t)
	cfg := testTokenConfig(t, db)
	srv := server.New(cfg)

	plaintext, err := index.CreateToken(db, "bob", "", 0)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	// Get the hash prefix and revoke.
	tokens, err := index.ListTokens(db, "bob")
	if err != nil {
		t.Fatalf("ListTokens: %v", err)
	}
	_, err = index.RevokeTokenByHashPrefix(db, tokens[0].Hash[:12])
	if err != nil {
		t.Fatalf("RevokeTokenByHashPrefix: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401", w.Code)
	}

	var p server.Problem
	if err := json.NewDecoder(w.Body).Decode(&p); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if p.Status != http.StatusUnauthorized {
		t.Errorf("problem.status = %d; want 401", p.Status)
	}
}
