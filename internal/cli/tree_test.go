package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/config"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/storage"
	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// initMinimalRepo sets up the minimal .decisions/ structure required by tree
// commands without running dtree init (so tests don't need an interactive actor
// setup). Returns repoRoot.
func initMinimalRepo(t *testing.T) string {
	t.Helper()
	repoRoot, xdgDir := isolatedEnv(t)

	decisionsDir := filepath.Join(repoRoot, ".decisions")
	if err := os.MkdirAll(filepath.Join(decisionsDir, "audit"), 0o755); err != nil {
		t.Fatalf("mkdir audit: %v", err)
	}

	// Write actors.yaml with a test actor.
	af := &storage.ActorsFile{
		Actors: []core.Actor{
			{Handle: "testactor", Name: "Test Actor", Email: "test@example.com", Kind: core.ActorHuman, Active: true},
		},
	}
	if err := storage.WriteActors(filepath.Join(decisionsDir, storage.ActorsFileName), af); err != nil {
		t.Fatalf("write actors.yaml: %v", err)
	}

	// Write trees.yaml (empty).
	tf := &storage.TreesFile{}
	if err := storage.WriteTrees(filepath.Join(decisionsDir, storage.TreesFileName), tf); err != nil {
		t.Fatalf("write trees.yaml: %v", err)
	}

	// Write config.yaml.
	localCfg := &config.File{Identity: config.IdentityConfig{Default: "testactor"}}
	if err := config.WriteFile(config.LocalPath(repoRoot), localCfg); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	// Open index to initialize schema.
	db, err := index.Open(filepath.Join(decisionsDir, ".index.db"))
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	// Insert the test actor so identity.MustResolve succeeds.
	if _, err := db.Conn().Exec(
		`INSERT OR IGNORE INTO actors(handle,name,email,kind,active) VALUES(?,?,?,?,?)`,
		"testactor", "Test Actor", "test@example.com", "human", 1,
	); err != nil {
		t.Fatalf("insert actor: %v", err)
	}
	db.Close()

	// Set global config identity so resolution works across layers.
	globalPath := filepath.Join(xdgDir, "dtree", "config.yaml")
	globalCfg := &config.File{Identity: config.IdentityConfig{Default: "testactor"}}
	if err := config.WriteFile(globalPath, globalCfg); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	return repoRoot
}

// ---------------------------------------------------------------------------
// tree create
// ---------------------------------------------------------------------------

func TestCreateTree(t *testing.T) {
	repoRoot := initMinimalRepo(t)

	out, _, err := runCmd(t, "--repo-root", repoRoot, "tree", "create", "backend")
	if err != nil {
		t.Fatalf("tree create failed: %v", err)
	}
	if !strings.Contains(out, "backend") {
		t.Errorf("expected output to mention 'backend', got: %q", out)
	}

	decisionsDir := filepath.Join(repoRoot, ".decisions")

	// Check directory structure.
	for _, rel := range []string{
		filepath.Join("backend", "decisions"),
		filepath.Join("backend", "audit"),
		filepath.Join("backend", storage.TreeMetaFileName),
	} {
		if _, err := os.Stat(filepath.Join(decisionsDir, rel)); os.IsNotExist(err) {
			t.Errorf("expected %s to exist", rel)
		}
	}

	// Check tree.yaml.
	tree, err := storage.ReadTree(filepath.Join(decisionsDir, "backend", storage.TreeMetaFileName))
	if err != nil {
		t.Fatalf("ReadTree: %v", err)
	}
	if tree.Slug != "backend" {
		t.Errorf("tree.Slug: got %q, want %q", tree.Slug, "backend")
	}
	if tree.CreatedAt.IsZero() {
		t.Error("tree.CreatedAt should not be zero")
	}

	// Check trees.yaml.
	tf, err := storage.ReadTrees(filepath.Join(decisionsDir, storage.TreesFileName))
	if err != nil {
		t.Fatalf("ReadTrees: %v", err)
	}
	if len(tf.Trees) != 1 || tf.Trees[0] != "backend" {
		t.Errorf("trees.yaml: got %v, want [backend]", tf.Trees)
	}

	// Check index row.
	db, err := index.Open(filepath.Join(decisionsDir, ".index.db"))
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	defer db.Close()

	var slug string
	if err := db.Conn().QueryRow(`SELECT slug FROM trees WHERE slug=?`, "backend").Scan(&slug); err != nil {
		t.Fatalf("index tree row: %v", err)
	}
	if slug != "backend" {
		t.Errorf("index slug: got %q, want %q", slug, "backend")
	}

	// Check audit event.
	events, err := audit.Read(repoRoot, audit.Filter{Action: core.ActionTreeCreate})
	if err != nil {
		t.Fatalf("audit.Read: %v", err)
	}
	var found bool
	for _, ev := range events {
		if ev.ID == "backend" && ev.Action == core.ActionTreeCreate {
			found = true
		}
	}
	if !found {
		t.Error("expected tree_create audit event for 'backend'")
	}
}

func TestCreateTreeWithTitle(t *testing.T) {
	repoRoot := initMinimalRepo(t)

	_, _, err := runCmd(t, "--repo-root", repoRoot, "tree", "create", "frontend",
		"--title", "Frontend Decisions",
		"--description", "UI-related decisions")
	if err != nil {
		t.Fatalf("tree create failed: %v", err)
	}

	tree, err := storage.ReadTree(filepath.Join(repoRoot, ".decisions", "frontend", storage.TreeMetaFileName))
	if err != nil {
		t.Fatalf("ReadTree: %v", err)
	}
	if tree.Title != "Frontend Decisions" {
		t.Errorf("tree.Title: got %q, want %q", tree.Title, "Frontend Decisions")
	}
	if tree.Description != "UI-related decisions" {
		t.Errorf("tree.Description: got %q, want %q", tree.Description, "UI-related decisions")
	}
}

func TestCreateTreeInvalidSlug(t *testing.T) {
	repoRoot := initMinimalRepo(t)

	cases := []string{
		"Foo Bar",   // uppercase + space
		"1abc",      // starts with digit
		"",          // empty
		"-abc",      // starts with dash
		"UPPER",     // uppercase
		"has space", // space
	}

	for _, slug := range cases {
		t.Run(slug, func(t *testing.T) {
			args := []string{"--repo-root", repoRoot, "tree", "create"}
			if slug != "" {
				args = append(args, slug)
			}
			_, _, err := runCmd(t, args...)
			if err == nil {
				t.Errorf("expected error for slug %q, got nil", slug)
			}
		})
	}
}

func TestCreateTreeAlreadyExists(t *testing.T) {
	repoRoot := initMinimalRepo(t)

	// Create the tree once.
	_, _, err := runCmd(t, "--repo-root", repoRoot, "tree", "create", "dup")
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}

	// Second create should fail.
	_, _, err = runCmd(t, "--repo-root", repoRoot, "tree", "create", "dup")
	if err == nil {
		t.Fatal("expected error on duplicate create, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists': %v", err)
	}
}

func TestCreateTreeNoDecisionsDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	t.Setenv("DTREE_AS", "")
	t.Setenv("DTREE_TREE", "")

	_, _, err := runCmd(t, "--repo-root", dir, "tree", "create", "myslug")
	if err == nil {
		t.Fatal("expected error when .decisions/ missing, got nil")
	}
	if !strings.Contains(err.Error(), ".decisions/") {
		t.Errorf("error should mention .decisions/: %v", err)
	}
}

// ---------------------------------------------------------------------------
// tree list
// ---------------------------------------------------------------------------

func TestList(t *testing.T) {
	repoRoot := initMinimalRepo(t)

	// Create a few trees.
	for _, slug := range []string{"zebra", "alpha", "middle"} {
		if _, _, err := runCmd(t, "--repo-root", repoRoot, "tree", "create", slug); err != nil {
			t.Fatalf("create %s: %v", slug, err)
		}
	}

	out, _, err := runCmd(t, "--repo-root", repoRoot, "--output", "human", "tree", "list")
	if err != nil {
		t.Fatalf("tree list failed: %v", err)
	}

	// All three should appear in stable order.
	alphaIdx := strings.Index(out, "alpha")
	middleIdx := strings.Index(out, "middle")
	zebraIdx := strings.Index(out, "zebra")

	if alphaIdx < 0 || middleIdx < 0 || zebraIdx < 0 {
		t.Errorf("expected all trees in output, got:\n%s", out)
	}
	if !(alphaIdx < middleIdx && middleIdx < zebraIdx) {
		t.Errorf("expected stable alphabetical order (alpha < middle < zebra), got positions: %d %d %d",
			alphaIdx, middleIdx, zebraIdx)
	}
}

func TestListExcludesArchived(t *testing.T) {
	repoRoot := initMinimalRepo(t)

	if _, _, err := runCmd(t, "--repo-root", repoRoot, "tree", "create", "active"); err != nil {
		t.Fatalf("create active: %v", err)
	}
	if _, _, err := runCmd(t, "--repo-root", repoRoot, "tree", "create", "toarchive"); err != nil {
		t.Fatalf("create toarchive: %v", err)
	}
	if _, _, err := runCmd(t, "--repo-root", repoRoot, "tree", "archive", "toarchive"); err != nil {
		t.Fatalf("archive toarchive: %v", err)
	}

	// Default list: should not show archived.
	out, _, err := runCmd(t, "--repo-root", repoRoot, "--output", "human", "tree", "list")
	if err != nil {
		t.Fatalf("tree list failed: %v", err)
	}
	if strings.Contains(out, "toarchive") {
		t.Errorf("archived tree should not appear in default list, got:\n%s", out)
	}
	if !strings.Contains(out, "active") {
		t.Errorf("active tree should appear in list, got:\n%s", out)
	}

	// --include-archived: should show all.
	out2, _, err2 := runCmd(t, "--repo-root", repoRoot, "--output", "human", "tree", "list", "--include-archived")
	if err2 != nil {
		t.Fatalf("tree list --include-archived failed: %v", err2)
	}
	if !strings.Contains(out2, "toarchive") {
		t.Errorf("archived tree should appear with --include-archived, got:\n%s", out2)
	}
}

func TestListJSON(t *testing.T) {
	repoRoot := initMinimalRepo(t)

	if _, _, err := runCmd(t, "--repo-root", repoRoot, "tree", "create", "myapp"); err != nil {
		t.Fatalf("create myapp: %v", err)
	}

	out, _, err := runCmd(t, "--repo-root", repoRoot, "--output", "json", "tree", "list")
	if err != nil {
		t.Fatalf("tree list --output json failed: %v", err)
	}

	var parsed []struct {
		Slug          string `json:"slug"`
		DecisionCount int    `json:"decision_count"`
		Archived      bool   `json:"archived"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid JSON output: %v\noutput:\n%s", err, out)
	}
	if len(parsed) != 1 || parsed[0].Slug != "myapp" {
		t.Errorf("expected [{slug:myapp}], got %+v", parsed)
	}
}

func TestListYAML(t *testing.T) {
	repoRoot := initMinimalRepo(t)

	if _, _, err := runCmd(t, "--repo-root", repoRoot, "tree", "create", "yamlapp"); err != nil {
		t.Fatalf("create yamlapp: %v", err)
	}

	out, _, err := runCmd(t, "--repo-root", repoRoot, "--output", "yaml", "tree", "list")
	if err != nil {
		t.Fatalf("tree list --output yaml failed: %v", err)
	}

	var parsed []struct {
		Slug string `yaml:"slug"`
	}
	if err := yaml.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid YAML output: %v\noutput:\n%s", err, out)
	}
	if len(parsed) != 1 || parsed[0].Slug != "yamlapp" {
		t.Errorf("expected [{slug:yamlapp}], got %+v", parsed)
	}
}

// ---------------------------------------------------------------------------
// tree show
// ---------------------------------------------------------------------------

func TestShow(t *testing.T) {
	repoRoot := initMinimalRepo(t)

	if _, _, err := runCmd(t, "--repo-root", repoRoot, "tree", "create", "showme",
		"--title", "Show Me",
		"--description", "A test tree"); err != nil {
		t.Fatalf("create showme: %v", err)
	}

	out, _, err := runCmd(t, "--repo-root", repoRoot, "--output", "human", "tree", "show", "showme")
	if err != nil {
		t.Fatalf("tree show failed: %v", err)
	}

	if !strings.Contains(out, "showme") {
		t.Errorf("output should contain slug: %q", out)
	}
	if !strings.Contains(out, "Show Me") {
		t.Errorf("output should contain title: %q", out)
	}
}

func TestShowJSON(t *testing.T) {
	repoRoot := initMinimalRepo(t)

	if _, _, err := runCmd(t, "--repo-root", repoRoot, "tree", "create", "showjson",
		"--title", "JSON Tree"); err != nil {
		t.Fatalf("create showjson: %v", err)
	}

	out, _, err := runCmd(t, "--repo-root", repoRoot, "--output", "json", "tree", "show", "showjson")
	if err != nil {
		t.Fatalf("tree show --output json failed: %v", err)
	}

	var parsed struct {
		Slug          string    `json:"slug"`
		Title         string    `json:"title"`
		Archived      bool      `json:"archived"`
		CreatedAt     time.Time `json:"created_at"`
		DecisionCount int       `json:"decision_count"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\noutput:\n%s", err, out)
	}
	if parsed.Slug != "showjson" {
		t.Errorf("slug: got %q, want %q", parsed.Slug, "showjson")
	}
	if parsed.Title != "JSON Tree" {
		t.Errorf("title: got %q, want %q", parsed.Title, "JSON Tree")
	}
	if parsed.Archived {
		t.Error("archived should be false")
	}
	if parsed.CreatedAt.IsZero() {
		t.Error("created_at should not be zero")
	}
}

// ---------------------------------------------------------------------------
// tree rename
// ---------------------------------------------------------------------------

func TestRename(t *testing.T) {
	repoRoot := initMinimalRepo(t)

	if _, _, err := runCmd(t, "--repo-root", repoRoot, "tree", "create", "oldname"); err != nil {
		t.Fatalf("create oldname: %v", err)
	}

	out, _, err := runCmd(t, "--repo-root", repoRoot, "tree", "rename", "oldname", "newname")
	if err != nil {
		t.Fatalf("tree rename failed: %v", err)
	}

	if !strings.Contains(out, "oldname") || !strings.Contains(out, "newname") {
		t.Errorf("output should mention both names: %q", out)
	}

	decisionsDir := filepath.Join(repoRoot, ".decisions")

	// Old directory should be gone.
	if _, err := os.Stat(filepath.Join(decisionsDir, "oldname")); !os.IsNotExist(err) {
		t.Error("old directory should no longer exist")
	}

	// New directory should exist.
	if _, err := os.Stat(filepath.Join(decisionsDir, "newname")); os.IsNotExist(err) {
		t.Error("new directory should exist")
	}

	// tree.yaml should have new slug.
	tree, err := storage.ReadTree(filepath.Join(decisionsDir, "newname", storage.TreeMetaFileName))
	if err != nil {
		t.Fatalf("ReadTree: %v", err)
	}
	if tree.Slug != "newname" {
		t.Errorf("tree.Slug: got %q, want %q", tree.Slug, "newname")
	}

	// trees.yaml should list newname, not oldname.
	tf, err := storage.ReadTrees(filepath.Join(decisionsDir, storage.TreesFileName))
	if err != nil {
		t.Fatalf("ReadTrees: %v", err)
	}
	for _, s := range tf.Trees {
		if s == "oldname" {
			t.Error("trees.yaml should not contain oldname")
		}
	}
	found := false
	for _, s := range tf.Trees {
		if s == "newname" {
			found = true
		}
	}
	if !found {
		t.Error("trees.yaml should contain newname")
	}

	// Index should have new slug.
	db, err := index.Open(filepath.Join(decisionsDir, ".index.db"))
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	defer db.Close()

	var count int
	db.Conn().QueryRow(`SELECT COUNT(*) FROM trees WHERE slug=?`, "newname").Scan(&count)
	if count != 1 {
		t.Errorf("index should have 1 row for newname, got %d", count)
	}
	db.Conn().QueryRow(`SELECT COUNT(*) FROM trees WHERE slug=?`, "oldname").Scan(&count)
	if count != 0 {
		t.Errorf("index should have 0 rows for oldname, got %d", count)
	}

	// Audit event.
	events, err := audit.Read(repoRoot, audit.Filter{Action: core.ActionTreeRename})
	if err != nil {
		t.Fatalf("audit.Read: %v", err)
	}
	var foundEvent bool
	for _, ev := range events {
		if ev.ID == "newname" && ev.Action == core.ActionTreeRename {
			foundEvent = true
			if before, ok := ev.Payload.Before["slug"].(string); !ok || before != "oldname" {
				t.Errorf("audit event before.slug: got %v, want oldname", ev.Payload.Before["slug"])
			}
			if after, ok := ev.Payload.After["slug"].(string); !ok || after != "newname" {
				t.Errorf("audit event after.slug: got %v, want newname", ev.Payload.After["slug"])
			}
		}
	}
	if !foundEvent {
		t.Error("expected tree_rename audit event")
	}
}

func TestRenameArchivedRefused(t *testing.T) {
	repoRoot := initMinimalRepo(t)

	if _, _, err := runCmd(t, "--repo-root", repoRoot, "tree", "create", "archtree"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, _, err := runCmd(t, "--repo-root", repoRoot, "tree", "archive", "archtree"); err != nil {
		t.Fatalf("archive: %v", err)
	}

	_, _, err := runCmd(t, "--repo-root", repoRoot, "tree", "rename", "archtree", "newtree")
	if err == nil {
		t.Fatal("expected error renaming archived tree, got nil")
	}
	if !strings.Contains(err.Error(), "archived") {
		t.Errorf("error should mention 'archived': %v", err)
	}
}

func TestRenameToExistingSlugRefused(t *testing.T) {
	repoRoot := initMinimalRepo(t)

	for _, slug := range []string{"treea", "treeb"} {
		if _, _, err := runCmd(t, "--repo-root", repoRoot, "tree", "create", slug); err != nil {
			t.Fatalf("create %s: %v", slug, err)
		}
	}

	_, _, err := runCmd(t, "--repo-root", repoRoot, "tree", "rename", "treea", "treeb")
	if err == nil {
		t.Fatal("expected error renaming to existing slug, got nil")
	}
}

// ---------------------------------------------------------------------------
// tree archive
// ---------------------------------------------------------------------------

func TestArchive(t *testing.T) {
	repoRoot := initMinimalRepo(t)

	if _, _, err := runCmd(t, "--repo-root", repoRoot, "tree", "create", "arch"); err != nil {
		t.Fatalf("create: %v", err)
	}

	out, _, err := runCmd(t, "--repo-root", repoRoot, "tree", "archive", "arch")
	if err != nil {
		t.Fatalf("tree archive failed: %v", err)
	}
	if !strings.Contains(out, "arch") {
		t.Errorf("output should mention slug: %q", out)
	}

	// tree.yaml should be archived.
	tree, err := storage.ReadTree(filepath.Join(repoRoot, ".decisions", "arch", storage.TreeMetaFileName))
	if err != nil {
		t.Fatalf("ReadTree: %v", err)
	}
	if !tree.Archived {
		t.Error("tree.Archived should be true")
	}

	// Index should reflect archived=1.
	db, err := index.Open(filepath.Join(repoRoot, ".decisions", ".index.db"))
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	defer db.Close()

	var archived int
	db.Conn().QueryRow(`SELECT archived FROM trees WHERE slug=?`, "arch").Scan(&archived)
	if archived != 1 {
		t.Errorf("index archived: got %d, want 1", archived)
	}

	// Audit event.
	events, err := audit.Read(repoRoot, audit.Filter{Action: core.ActionTreeArchive})
	if err != nil {
		t.Fatalf("audit.Read: %v", err)
	}
	var found bool
	for _, ev := range events {
		if ev.ID == "arch" && ev.Action == core.ActionTreeArchive {
			found = true
		}
	}
	if !found {
		t.Error("expected tree_archive audit event")
	}
}

func TestArchiveAlreadyArchived(t *testing.T) {
	repoRoot := initMinimalRepo(t)

	if _, _, err := runCmd(t, "--repo-root", repoRoot, "tree", "create", "alrarch"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, _, err := runCmd(t, "--repo-root", repoRoot, "tree", "archive", "alrarch"); err != nil {
		t.Fatalf("first archive: %v", err)
	}

	_, _, err := runCmd(t, "--repo-root", repoRoot, "tree", "archive", "alrarch")
	if err == nil {
		t.Fatal("expected error archiving already-archived tree, got nil")
	}
	if !strings.Contains(err.Error(), "already archived") {
		t.Errorf("error should mention 'already archived': %v", err)
	}
}

// ---------------------------------------------------------------------------
// tree delete
// ---------------------------------------------------------------------------

func TestDeleteRequiresConfirmation(t *testing.T) {
	repoRoot := initMinimalRepo(t)

	if _, _, err := runCmd(t, "--repo-root", repoRoot, "tree", "create", "todelete"); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Missing --force.
	_, _, err := runCmd(t, "--repo-root", repoRoot, "tree", "delete", "todelete",
		"--confirm-name", "todelete")
	if err == nil {
		t.Fatal("expected error without --force")
	}

	// Missing --confirm-name.
	_, _, err = runCmd(t, "--repo-root", repoRoot, "tree", "delete", "todelete",
		"--force")
	if err == nil {
		t.Fatal("expected error without --confirm-name")
	}

	// Mismatched --confirm-name.
	_, _, err = runCmd(t, "--repo-root", repoRoot, "tree", "delete", "todelete",
		"--force", "--confirm-name", "wrongname")
	if err == nil {
		t.Fatal("expected error with mismatched --confirm-name")
	}
	if !strings.Contains(err.Error(), "does not match") {
		t.Errorf("error should mention mismatch: %v", err)
	}
}

func TestDeleteEmptyTree(t *testing.T) {
	repoRoot := initMinimalRepo(t)

	if _, _, err := runCmd(t, "--repo-root", repoRoot, "tree", "create", "empty"); err != nil {
		t.Fatalf("create: %v", err)
	}

	out, _, err := runCmd(t, "--repo-root", repoRoot, "tree", "delete", "empty",
		"--force", "--confirm-name", "empty")
	if err != nil {
		t.Fatalf("tree delete failed: %v", err)
	}
	if !strings.Contains(out, "empty") {
		t.Errorf("output should mention slug: %q", out)
	}

	decisionsDir := filepath.Join(repoRoot, ".decisions")

	// Directory should be gone.
	if _, err := os.Stat(filepath.Join(decisionsDir, "empty")); !os.IsNotExist(err) {
		t.Error("tree directory should be removed")
	}

	// trees.yaml should not list it.
	tf, err := storage.ReadTrees(filepath.Join(decisionsDir, storage.TreesFileName))
	if err != nil {
		t.Fatalf("ReadTrees: %v", err)
	}
	for _, s := range tf.Trees {
		if s == "empty" {
			t.Error("trees.yaml should not contain deleted tree")
		}
	}

	// Index should not have it.
	db, err := index.Open(filepath.Join(decisionsDir, ".index.db"))
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	defer db.Close()

	var count int
	db.Conn().QueryRow(`SELECT COUNT(*) FROM trees WHERE slug=?`, "empty").Scan(&count)
	if count != 0 {
		t.Errorf("index should have 0 rows for deleted tree, got %d", count)
	}

	// Audit event.
	events, err := audit.Read(repoRoot, audit.Filter{Action: core.ActionTreeDelete})
	if err != nil {
		t.Fatalf("audit.Read: %v", err)
	}
	var found bool
	for _, ev := range events {
		if ev.ID == "empty" && ev.Action == core.ActionTreeDelete {
			found = true
		}
	}
	if !found {
		t.Error("expected tree_delete audit event")
	}
}

func TestDeleteWithDecisionsRequiresCascade(t *testing.T) {
	repoRoot := initMinimalRepo(t)

	if _, _, err := runCmd(t, "--repo-root", repoRoot, "tree", "create", "withdecs"); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Manually insert a decision into the index to simulate a non-empty tree.
	db, err := index.Open(filepath.Join(repoRoot, ".decisions", ".index.db"))
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	_, dbErr := db.Conn().Exec(`
		INSERT INTO decisions(id, tree, slug, summary, description, status, priority, creator,
		                      assignee, recommended_summary, recommended_full, recommended_by,
		                      actual_choice, actual_choice_reason, is_recommended,
		                      out_of_scope_reason, schema_version, rev, content_sha256, deleted)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"01JXXXXXXXXXXXXXXXXXXXXXXXXX", "withdecs", "test-slug", "Test Decision", "",
		"proposed", "medium", "testactor", "", "", "", "", "", "", 0, "", 1, "rev1", "", 0,
	)
	db.Close()
	if dbErr != nil {
		t.Fatalf("insert decision: %v", dbErr)
	}

	// Without --cascade-decisions: should fail.
	_, _, err = runCmd(t, "--repo-root", repoRoot, "tree", "delete", "withdecs",
		"--force", "--confirm-name", "withdecs")
	if err == nil {
		t.Fatal("expected error without --cascade-decisions")
	}
	if !strings.Contains(err.Error(), "cascade-decisions") {
		t.Errorf("error should mention --cascade-decisions: %v", err)
	}

	// With --cascade-decisions: should succeed.
	out, _, err := runCmd(t, "--repo-root", repoRoot, "tree", "delete", "withdecs",
		"--force", "--confirm-name", "withdecs", "--cascade-decisions")
	if err != nil {
		t.Fatalf("tree delete with --cascade-decisions failed: %v", err)
	}
	if !strings.Contains(out, "withdecs") {
		t.Errorf("output should mention slug: %q", out)
	}

	// Verify the directory is gone.
	if _, err := os.Stat(filepath.Join(repoRoot, ".decisions", "withdecs")); !os.IsNotExist(err) {
		t.Error("tree directory should be removed")
	}

	// Audit event should be present.
	events, err := audit.Read(repoRoot, audit.Filter{Action: core.ActionTreeDelete})
	if err != nil {
		t.Fatalf("audit.Read: %v", err)
	}
	var found bool
	for _, ev := range events {
		if ev.ID == "withdecs" && ev.Action == core.ActionTreeDelete {
			found = true
		}
	}
	if !found {
		t.Error("expected tree_delete audit event")
	}
}
