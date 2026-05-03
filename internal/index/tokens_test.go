package index

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestCreateAndLookupToken(t *testing.T) {
	db := openTestDB(t)

	plaintext, err := CreateToken(db, "alice", "my-token", 0)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	if plaintext == "" {
		t.Fatal("CreateToken returned empty plaintext")
	}

	tok, err := LookupToken(db, plaintext)
	if err != nil {
		t.Fatalf("LookupToken: %v", err)
	}
	if tok.Handle != "alice" {
		t.Errorf("handle = %q, want alice", tok.Handle)
	}
	if tok.Label != "my-token" {
		t.Errorf("label = %q, want my-token", tok.Label)
	}
	if tok.Revoked {
		t.Error("expected not revoked")
	}
	if tok.ExpiresAt != nil {
		t.Error("expected nil ExpiresAt for no-ttl token")
	}
}

func TestLookupRevokedToken(t *testing.T) {
	db := openTestDB(t)

	plaintext, err := CreateToken(db, "bob", "", 0)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	// Get the full hash so we can revoke it.
	toks, err := ListTokens(db, "bob")
	if err != nil {
		t.Fatalf("ListTokens: %v", err)
	}
	if len(toks) != 1 {
		t.Fatalf("expected 1 token, got %d", len(toks))
	}
	prefix := toks[0].Hash[:12]

	_, err = RevokeTokenByHashPrefix(db, prefix)
	if err != nil {
		t.Fatalf("RevokeTokenByHashPrefix: %v", err)
	}

	_, err = LookupToken(db, plaintext)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("LookupToken on revoked token: got %v, want sql.ErrNoRows", err)
	}
}

func TestLookupExpiredToken(t *testing.T) {
	db := openTestDB(t)

	// Create a token with a very short TTL (already expired by the time we look it up).
	plaintext, err := CreateToken(db, "carol", "", -1*time.Second)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	_, err = LookupToken(db, plaintext)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("LookupToken on expired token: got %v, want sql.ErrNoRows", err)
	}
}

func TestRevokeByPrefix(t *testing.T) {
	db := openTestDB(t)

	_, err := CreateToken(db, "dave", "label-a", 0)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	toks, err := ListTokens(db, "dave")
	if err != nil {
		t.Fatalf("ListTokens: %v", err)
	}
	if len(toks) != 1 {
		t.Fatalf("expected 1 token, got %d", len(toks))
	}

	prefix := toks[0].Hash[:8]
	full, err := RevokeTokenByHashPrefix(db, prefix)
	if err != nil {
		t.Fatalf("RevokeTokenByHashPrefix: %v", err)
	}
	if full != toks[0].Hash {
		t.Errorf("full hash = %q, want %q", full, toks[0].Hash)
	}

	// Verify it's revoked in the DB.
	updated, err := ListTokens(db, "dave")
	if err != nil {
		t.Fatalf("ListTokens after revoke: %v", err)
	}
	if !updated[0].Revoked {
		t.Error("expected token to be marked revoked")
	}

	// Revoking again (already revoked) should return 0 matches.
	_, err = RevokeTokenByHashPrefix(db, prefix)
	if err == nil {
		t.Error("expected error revoking already-revoked token by prefix, got nil")
	}
}
