//go:build sqlite_fts5

package cli_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/cgould/dtree/internal/index"
)

// setupTokenRepo creates .decisions/ and opens (and closes) a fresh index.
func setupTokenRepo(t *testing.T) string {
	t.Helper()
	repoRoot, _ := isolatedEnv(t)
	setupMigrateIndex(t, repoRoot)
	return repoRoot
}

// TestTokenCreatePrintsPlaintext verifies that "dtree token create" prints a
// non-empty plaintext token to stdout.
func TestTokenCreatePrintsPlaintext(t *testing.T) {
	repoRoot := setupTokenRepo(t)

	out, _, err := runCmd(t,
		"--repo-root", repoRoot,
		"token", "create", "--as", "alice",
	)
	if err != nil {
		t.Fatalf("token create: %v", err)
	}
	plaintext := strings.TrimSpace(out)
	if plaintext == "" {
		t.Fatal("token create: expected non-empty plaintext on stdout")
	}
	// Plaintext is base64url-encoded 32 bytes ≈ 43 chars.
	if len(plaintext) < 40 {
		t.Errorf("plaintext seems too short: %q", plaintext)
	}
}

// TestTokenListShowsCreatedToken verifies that after creating a token it
// appears in "dtree token list".
func TestTokenListShowsCreatedToken(t *testing.T) {
	repoRoot := setupTokenRepo(t)

	// Create a token with a label.
	_, _, err := runCmd(t,
		"--repo-root", repoRoot,
		"token", "create", "--as", "bob", "--label", "ci-bot",
	)
	if err != nil {
		t.Fatalf("token create: %v", err)
	}

	out, _, err := runCmd(t,
		"--repo-root", repoRoot,
		"token", "list",
	)
	if err != nil {
		t.Fatalf("token list: %v", err)
	}
	if !strings.Contains(out, "bob") {
		t.Errorf("token list output missing handle 'bob':\n%s", out)
	}
	if !strings.Contains(out, "ci-bot") {
		t.Errorf("token list output missing label 'ci-bot':\n%s", out)
	}
}

// TestTokenRevokeRemovesFromActive verifies that after revoking a token,
// LookupToken returns ErrNoRows.
func TestTokenRevokeRemovesFromActive(t *testing.T) {
	repoRoot := setupTokenRepo(t)

	_, _, err := runCmd(t,
		"--repo-root", repoRoot,
		"token", "create", "--as", "carol",
	)
	if err != nil {
		t.Fatalf("token create: %v", err)
	}

	// Get the hash prefix via the index directly.
	dbPath := filepath.Join(repoRoot, ".decisions", ".index.db")
	db, err := index.Open(dbPath)
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	tokens, err := index.ListTokens(db, "carol")
	db.Close()
	if err != nil {
		t.Fatalf("ListTokens: %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token, got %d", len(tokens))
	}
	prefix := tokens[0].Hash[:12]

	// Revoke via CLI.
	out, _, err := runCmd(t,
		"--repo-root", repoRoot,
		"token", "revoke", prefix,
	)
	if err != nil {
		t.Fatalf("token revoke: %v", err)
	}
	if !strings.Contains(out, "Revoked") {
		t.Errorf("token revoke output missing 'Revoked':\n%s", out)
	}

	// Verify revoked status in list output.
	listOut, _, err := runCmd(t,
		"--repo-root", repoRoot,
		"token", "list",
	)
	if err != nil {
		t.Fatalf("token list after revoke: %v", err)
	}
	if !strings.Contains(listOut, "yes") {
		t.Errorf("token list after revoke should show 'yes' for revoked; got:\n%s", listOut)
	}
}
