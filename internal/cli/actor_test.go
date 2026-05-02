package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/config"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/storage"
	"github.com/cgould/dtree/internal/ulid"
	"gopkg.in/yaml.v3"
)

// setupActorEnv initialises a repo with .decisions/ and a SQLite index, but
// no actors.yaml yet. It returns repoRoot and the open *index.DB.
func setupActorEnv(t *testing.T) (repoRoot string, db *index.DB) {
	t.Helper()
	repoRoot, _ = setupTestEnv(t) // sets XDG_CONFIG_HOME, DTREE_AS, DTREE_TREE

	decisionsDir := filepath.Join(repoRoot, ".decisions")
	auditDir := filepath.Join(decisionsDir, "audit")
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		t.Fatalf("mkdir audit: %v", err)
	}

	indexPath := filepath.Join(decisionsDir, ".index.db")
	var err error
	db, err = index.Open(indexPath)
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return repoRoot, db
}

// readActorsFile is a helper to read actors.yaml in tests.
func readActorsFile(t *testing.T, repoRoot string) *storage.ActorsFile {
	t.Helper()
	path := filepath.Join(repoRoot, ".decisions", storage.ActorsFileName)
	af, err := storage.ReadActors(path)
	if err != nil {
		t.Fatalf("ReadActors: %v", err)
	}
	return af
}

// indexActorRow queries a single actor row from the index.
func indexActorRow(t *testing.T, db *index.DB, handle string) (name, email, kind string, active int) {
	t.Helper()
	row := db.Conn().QueryRow(
		`SELECT name, email, kind, active FROM actors WHERE handle = ?`, handle,
	)
	if err := row.Scan(&name, &email, &kind, &active); err != nil {
		t.Fatalf("index actor row %q: %v", handle, err)
	}
	return
}

// ── TestActorAdd ─────────────────────────────────────────────────────────────

func TestActorAdd(t *testing.T) {
	repoRoot, db := setupActorEnv(t)

	out, _, err := runCmd(t,
		"--repo-root", repoRoot,
		"actor", "add", "alice",
		"--name", "Alice Smith",
		"--email", "alice@example.com",
		"--kind", "human",
	)
	if err != nil {
		t.Fatalf("actor add: %v", err)
	}
	if !strings.Contains(out, "alice") {
		t.Errorf("expected output to mention 'alice', got: %q", out)
	}

	// Verify actors.yaml.
	af := readActorsFile(t, repoRoot)
	if len(af.Actors) != 1 {
		t.Fatalf("expected 1 actor, got %d", len(af.Actors))
	}
	a := af.Actors[0]
	if a.Handle != "alice" || a.Name != "Alice Smith" || a.Email != "alice@example.com" {
		t.Errorf("actor fields: %+v", a)
	}
	if a.Kind != core.ActorHuman {
		t.Errorf("kind: got %q, want %q", a.Kind, core.ActorHuman)
	}
	if !a.Active {
		t.Error("actor should be active")
	}

	// Verify index row.
	name, email, kind, active := indexActorRow(t, db, "alice")
	if name != "Alice Smith" || email != "alice@example.com" || kind != "human" || active != 1 {
		t.Errorf("index row: name=%q email=%q kind=%q active=%d", name, email, kind, active)
	}

	// Verify audit event.
	events, err := audit.Read(repoRoot, audit.Filter{Action: core.ActionActorAdd})
	if err != nil {
		t.Fatalf("audit.Read: %v", err)
	}
	if len(events) == 0 {
		t.Error("expected actor_add audit event")
	}
	if events[0].ID != "alice" {
		t.Errorf("audit event ID: got %q, want %q", events[0].ID, "alice")
	}
}

func TestActorAddDefaultKind(t *testing.T) {
	repoRoot, db := setupActorEnv(t)

	_, _, err := runCmd(t,
		"--repo-root", repoRoot,
		"actor", "add", "bob",
		"--name", "Bob",
	)
	if err != nil {
		t.Fatalf("actor add: %v", err)
	}

	_, _, kind, _ := indexActorRow(t, db, "bob")
	if kind != "human" {
		t.Errorf("default kind: got %q, want %q", kind, "human")
	}
}

// ── TestActorAddInvalidHandle ─────────────────────────────────────────────────

func TestActorAddInvalidHandle(t *testing.T) {
	repoRoot, _ := setupActorEnv(t)

	_, _, err := runCmd(t,
		"--repo-root", repoRoot,
		"actor", "add", "0invalid", // starts with digit — invalid
		"--name", "Bad",
	)
	if err == nil {
		t.Fatal("expected error for invalid handle, got nil")
	}
	if !strings.Contains(err.Error(), "invalid handle") {
		t.Errorf("expected 'invalid handle' error, got: %v", err)
	}
}

// ── TestActorAddDuplicate ─────────────────────────────────────────────────────

func TestActorAddDuplicate(t *testing.T) {
	repoRoot, _ := setupActorEnv(t)

	// First add.
	if _, _, err := runCmd(t,
		"--repo-root", repoRoot,
		"actor", "add", "cam",
		"--name", "Cam",
	); err != nil {
		t.Fatalf("first add: %v", err)
	}

	// Second add with different name → conflict.
	_, _, err := runCmd(t,
		"--repo-root", repoRoot,
		"actor", "add", "cam",
		"--name", "Cam Different",
	)
	if err == nil {
		t.Fatal("expected error for duplicate actor, got nil")
	}
	if !strings.Contains(err.Error(), "actor already exists") {
		t.Errorf("expected 'actor already exists' error, got: %v", err)
	}
}

// ── TestActorList ─────────────────────────────────────────────────────────────

func TestActorList(t *testing.T) {
	repoRoot, _ := setupActorEnv(t)

	// Add two actors, one active, one archived.
	if _, _, err := runCmd(t, "--repo-root", repoRoot, "actor", "add", "alice", "--name", "Alice"); err != nil {
		t.Fatalf("add alice: %v", err)
	}
	if _, _, err := runCmd(t, "--repo-root", repoRoot, "actor", "add", "bob", "--name", "Bob"); err != nil {
		t.Fatalf("add bob: %v", err)
	}
	if _, _, err := runCmd(t, "--repo-root", repoRoot, "actor", "archive", "bob"); err != nil {
		t.Fatalf("archive bob: %v", err)
	}

	// Default list: should show alice but not bob.
	out, _, err := runCmd(t, "--repo-root", repoRoot, "--output", "human", "actor", "list")
	if err != nil {
		t.Fatalf("actor list: %v", err)
	}
	if !strings.Contains(out, "alice") {
		t.Errorf("expected alice in list output, got: %q", out)
	}
	if strings.Contains(out, "bob") {
		t.Errorf("expected bob NOT in default list (archived), got: %q", out)
	}

	// --include-archived: should show both.
	out2, _, err2 := runCmd(t, "--repo-root", repoRoot, "--output", "human", "actor", "list", "--include-archived")
	if err2 != nil {
		t.Fatalf("actor list --include-archived: %v", err2)
	}
	if !strings.Contains(out2, "alice") {
		t.Errorf("expected alice in archived list, got: %q", out2)
	}
	if !strings.Contains(out2, "bob") {
		t.Errorf("expected bob in archived list, got: %q", out2)
	}
}

// ── TestActorListJSON ─────────────────────────────────────────────────────────

func TestActorListJSON(t *testing.T) {
	repoRoot, _ := setupActorEnv(t)

	if _, _, err := runCmd(t, "--repo-root", repoRoot, "actor", "add", "alice", "--name", "Alice", "--email", "a@x.com"); err != nil {
		t.Fatalf("add alice: %v", err)
	}

	out, _, err := runCmd(t, "--repo-root", repoRoot, "--output", "json", "actor", "list")
	if err != nil {
		t.Fatalf("actor list json: %v", err)
	}

	var actors []struct {
		Handle string `json:"handle"`
		Name   string `json:"name"`
		Email  string `json:"email"`
		Kind   string `json:"kind"`
		Active bool   `json:"active"`
	}
	if err := json.Unmarshal([]byte(out), &actors); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, out)
	}
	if len(actors) != 1 || actors[0].Handle != "alice" {
		t.Errorf("expected [alice], got: %+v", actors)
	}
	if actors[0].Name != "Alice" {
		t.Errorf("name: got %q", actors[0].Name)
	}
	if !actors[0].Active {
		t.Error("expected active=true")
	}
}

// ── TestActorListYAML ─────────────────────────────────────────────────────────

func TestActorListYAML(t *testing.T) {
	repoRoot, _ := setupActorEnv(t)

	if _, _, err := runCmd(t, "--repo-root", repoRoot, "actor", "add", "cam", "--name", "Cam"); err != nil {
		t.Fatalf("add cam: %v", err)
	}

	out, _, err := runCmd(t, "--repo-root", repoRoot, "--output", "yaml", "actor", "list")
	if err != nil {
		t.Fatalf("actor list yaml: %v", err)
	}

	var actors []struct {
		Handle string `yaml:"handle"`
		Name   string `yaml:"name"`
	}
	if err := yaml.Unmarshal([]byte(out), &actors); err != nil {
		t.Fatalf("invalid YAML: %v\noutput: %s", err, out)
	}
	if len(actors) != 1 || actors[0].Handle != "cam" {
		t.Errorf("expected [cam], got: %+v", actors)
	}
}

// ── TestActorShow ─────────────────────────────────────────────────────────────

func TestActorShow(t *testing.T) {
	repoRoot, _ := setupActorEnv(t)

	if _, _, err := runCmd(t, "--repo-root", repoRoot, "actor", "add", "alice", "--name", "Alice"); err != nil {
		t.Fatalf("add: %v", err)
	}

	out, _, err := runCmd(t, "--repo-root", repoRoot, "--output", "human", "actor", "show", "alice")
	if err != nil {
		t.Fatalf("actor show: %v", err)
	}
	if !strings.Contains(out, "alice") {
		t.Errorf("expected 'alice' in show output, got: %q", out)
	}
}

func TestActorShowNotFound(t *testing.T) {
	repoRoot, _ := setupActorEnv(t)

	_, _, err := runCmd(t, "--repo-root", repoRoot, "--output", "human", "actor", "show", "nobody")
	if err == nil {
		t.Fatal("expected error for missing actor, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

// ── TestActorRenameUpdatesReferences ─────────────────────────────────────────

// setupTreeInIndex inserts a tree row into the index so decisions can reference it.
func setupTreeInIndex(t *testing.T, db *index.DB, slug string) {
	t.Helper()
	_, err := db.Conn().Exec(
		`INSERT OR IGNORE INTO trees(slug, title, description, archived, created_at, layout_direction, schema_version)
		 VALUES(?, '', '', 0, '2026-01-01T00:00:00Z', 'TB', 1)`,
		slug,
	)
	if err != nil {
		t.Fatalf("insert tree %s: %v", slug, err)
	}
}

func TestActorRenameUpdatesReferences(t *testing.T) {
	repoRoot, db := setupActorEnv(t)

	// Add two actors.
	for _, h := range []string{"oldcam", "witness"} {
		if _, _, err := runCmd(t, "--repo-root", repoRoot, "actor", "add", h, "--name", h); err != nil {
			t.Fatalf("add %s: %v", h, err)
		}
	}

	// Set up a tree directory and index row so InsertDecision doesn't fail FK constraint.
	treeSlug := "backend"
	treeDir := filepath.Join(repoRoot, ".decisions", treeSlug, "decisions")
	if err := os.MkdirAll(treeDir, 0o755); err != nil {
		t.Fatalf("mkdir treeDir: %v", err)
	}
	setupTreeInIndex(t, db, treeSlug)

	// Create a decision with creator=oldcam.
	decID := ulid.New()
	decSlug := "pick-db"
	d := &core.Decision{
		ID:            decID,
		Slug:          decSlug,
		Tree:          treeSlug,
		Summary:       "Pick database",
		Status:        core.StatusProposed,
		Priority:      core.PriorityHigh,
		Creator:       "oldcam",
		SchemaVersion: core.SchemaVersion,
	}

	// Write the YAML to disk.
	decPath := storage.DecisionPath(filepath.Join(repoRoot, ".decisions", treeSlug), decID, decSlug)
	if err := storage.WriteDecision(decPath, d); err != nil {
		t.Fatalf("write decision YAML: %v", err)
	}

	// Insert into index.
	if err := index.InsertDecision(db, d, "sha"); err != nil {
		t.Fatalf("InsertDecision: %v", err)
	}

	// Rename oldcam → newcam.
	out, _, err := runCmd(t,
		"--repo-root", repoRoot,
		"actor", "rename", "oldcam", "newcam",
	)
	if err != nil {
		t.Fatalf("actor rename: %v", err)
	}
	if !strings.Contains(out, "newcam") {
		t.Errorf("expected rename confirmation in output, got: %q", out)
	}

	// Verify actors.yaml no longer has oldcam.
	af := readActorsFile(t, repoRoot)
	for _, a := range af.Actors {
		if a.Handle == "oldcam" {
			t.Error("oldcam still in actors.yaml after rename")
		}
	}
	found := false
	for _, a := range af.Actors {
		if a.Handle == "newcam" {
			found = true
		}
	}
	if !found {
		t.Error("newcam not in actors.yaml after rename")
	}

	// Verify index: creator column updated.
	var creator string
	if err := db.Conn().QueryRow(`SELECT creator FROM decisions WHERE id = ?`, decID).Scan(&creator); err != nil {
		t.Fatalf("query creator: %v", err)
	}
	if creator != "newcam" {
		t.Errorf("index creator: got %q, want %q", creator, "newcam")
	}

	// Verify YAML on disk.
	diskD, err := storage.ReadDecision(decPath)
	if err != nil {
		t.Fatalf("ReadDecision: %v", err)
	}
	if diskD.Creator != "newcam" {
		t.Errorf("YAML creator: got %q, want %q", diskD.Creator, "newcam")
	}

	// Verify audit event.
	events, err := audit.Read(repoRoot, audit.Filter{Action: core.ActionActorRename})
	if err != nil {
		t.Fatalf("audit.Read: %v", err)
	}
	if len(events) == 0 {
		t.Error("expected actor_rename audit event")
	}
}

// ── TestActorRenameNotFound ───────────────────────────────────────────────────

func TestActorRenameNotFound(t *testing.T) {
	repoRoot, _ := setupActorEnv(t)

	_, _, err := runCmd(t, "--repo-root", repoRoot, "actor", "rename", "nobody", "newname")
	if err == nil {
		t.Fatal("expected error renaming non-existent actor, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

// ── TestActorRenameConflict ───────────────────────────────────────────────────

func TestActorRenameConflict(t *testing.T) {
	repoRoot, _ := setupActorEnv(t)

	for _, h := range []string{"alice", "bob"} {
		if _, _, err := runCmd(t, "--repo-root", repoRoot, "actor", "add", h, "--name", h); err != nil {
			t.Fatalf("add %s: %v", h, err)
		}
	}

	_, _, err := runCmd(t, "--repo-root", repoRoot, "actor", "rename", "alice", "bob")
	if err == nil {
		t.Fatal("expected error when target handle exists, got nil")
	}
	if !strings.Contains(err.Error(), "actor already exists") {
		t.Errorf("expected 'actor already exists' error, got: %v", err)
	}
}

// ── TestActorArchive ──────────────────────────────────────────────────────────

func TestActorArchive(t *testing.T) {
	repoRoot, db := setupActorEnv(t)

	if _, _, err := runCmd(t, "--repo-root", repoRoot, "actor", "add", "alice", "--name", "Alice"); err != nil {
		t.Fatalf("add: %v", err)
	}

	out, _, err := runCmd(t, "--repo-root", repoRoot, "actor", "archive", "alice")
	if err != nil {
		t.Fatalf("actor archive: %v", err)
	}
	if !strings.Contains(out, "alice") {
		t.Errorf("expected alice in archive output, got: %q", out)
	}

	// actors.yaml active flag.
	af := readActorsFile(t, repoRoot)
	if len(af.Actors) != 1 || af.Actors[0].Active {
		t.Errorf("expected active=false after archive, got: %+v", af.Actors)
	}

	// Index active=0.
	var active int
	if err := db.Conn().QueryRow(`SELECT active FROM actors WHERE handle = 'alice'`).Scan(&active); err != nil {
		t.Fatalf("index active: %v", err)
	}
	if active != 0 {
		t.Errorf("index active: got %d, want 0", active)
	}

	// Audit event.
	events, err := audit.Read(repoRoot, audit.Filter{Action: core.ActionActorArchive})
	if err != nil {
		t.Fatalf("audit.Read: %v", err)
	}
	if len(events) == 0 {
		t.Error("expected actor_archive audit event")
	}
}

// ── TestActorAlreadyArchived ──────────────────────────────────────────────────

func TestActorAlreadyArchived(t *testing.T) {
	repoRoot, _ := setupActorEnv(t)

	if _, _, err := runCmd(t, "--repo-root", repoRoot, "actor", "add", "alice", "--name", "Alice"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, _, err := runCmd(t, "--repo-root", repoRoot, "actor", "archive", "alice"); err != nil {
		t.Fatalf("first archive: %v", err)
	}

	_, _, err := runCmd(t, "--repo-root", repoRoot, "actor", "archive", "alice")
	if err == nil {
		t.Fatal("expected error archiving already-archived actor, got nil")
	}
	if !strings.Contains(err.Error(), "already archived") {
		t.Errorf("expected 'already archived' error, got: %v", err)
	}
}

// ── TestActorLinkFromGlobal ───────────────────────────────────────────────────

func TestActorLinkFromGlobal(t *testing.T) {
	repoRoot, db := setupActorEnv(t)

	// Pre-populate the global config catalog.
	xdgDir := os.Getenv("XDG_CONFIG_HOME")
	globalPath := filepath.Join(xdgDir, "dtree", "config.yaml")
	gf := &config.File{
		Identity: config.IdentityConfig{
			Identities: []core.Actor{
				{Handle: "globaluser", Name: "Global User", Email: "g@x.com", Kind: core.ActorHuman, Active: true},
			},
		},
	}
	if err := config.WriteFile(globalPath, gf); err != nil {
		t.Fatalf("WriteFile global: %v", err)
	}

	out, _, err := runCmd(t, "--repo-root", repoRoot, "actor", "link", "globaluser")
	if err != nil {
		t.Fatalf("actor link: %v", err)
	}
	if !strings.Contains(out, "globaluser") {
		t.Errorf("expected globaluser in link output, got: %q", out)
	}

	// actors.yaml should contain globaluser.
	af := readActorsFile(t, repoRoot)
	found := false
	for _, a := range af.Actors {
		if a.Handle == "globaluser" {
			found = true
			if a.Name != "Global User" {
				t.Errorf("name: got %q, want %q", a.Name, "Global User")
			}
		}
	}
	if !found {
		t.Error("globaluser not found in actors.yaml after link")
	}

	// Index should have the row.
	name, _, _, active := indexActorRow(t, db, "globaluser")
	if name != "Global User" || active != 1 {
		t.Errorf("index: name=%q active=%d", name, active)
	}

	// Audit event.
	events, err := audit.Read(repoRoot, audit.Filter{Action: core.ActionActorAdd})
	if err != nil {
		t.Fatalf("audit.Read: %v", err)
	}
	if len(events) == 0 {
		t.Error("expected actor_add audit event after link")
	}
}

// ── TestActorLinkUnknown ──────────────────────────────────────────────────────

func TestActorLinkUnknown(t *testing.T) {
	repoRoot, _ := setupActorEnv(t)

	// No global config catalog set up → no such identity.
	_, _, err := runCmd(t, "--repo-root", repoRoot, "actor", "link", "nobody")
	if err == nil {
		t.Fatal("expected error for unknown global identity, got nil")
	}
	if !strings.Contains(err.Error(), "no such global identity") {
		t.Errorf("expected 'no such global identity' error, got: %v", err)
	}
}

// ── TestActorLinkAlreadyRegistered ───────────────────────────────────────────

func TestActorLinkAlreadyRegistered(t *testing.T) {
	repoRoot, _ := setupActorEnv(t)

	// Add the actor locally.
	if _, _, err := runCmd(t, "--repo-root", repoRoot, "actor", "add", "localactor", "--name", "Local"); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Put the same handle in the global catalog.
	xdgDir := os.Getenv("XDG_CONFIG_HOME")
	globalPath := filepath.Join(xdgDir, "dtree", "config.yaml")
	gf := &config.File{
		Identity: config.IdentityConfig{
			Identities: []core.Actor{
				{Handle: "localactor", Name: "Global Copy", Email: "g@x.com", Kind: core.ActorHuman, Active: true},
			},
		},
	}
	if err := config.WriteFile(globalPath, gf); err != nil {
		t.Fatalf("WriteFile global: %v", err)
	}

	_, _, err := runCmd(t, "--repo-root", repoRoot, "actor", "link", "localactor")
	if err == nil {
		t.Fatal("expected error linking already-registered actor, got nil")
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Errorf("expected 'already registered' error, got: %v", err)
	}
}

// ── TestActorRequiresDecisionsDir ─────────────────────────────────────────────

func TestActorRequiresDecisionsDir(t *testing.T) {
	dir := t.TempDir()
	repoRoot := filepath.Join(dir, "norepo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	t.Setenv("DTREE_AS", "")
	t.Setenv("DTREE_TREE", "")

	_, _, err := runCmd(t, "--repo-root", repoRoot, "actor", "list")
	if err == nil {
		t.Fatal("expected error when .decisions/ missing, got nil")
	}
	if !strings.Contains(err.Error(), ".decisions/") {
		t.Errorf("expected error to mention .decisions/, got: %v", err)
	}
}
