package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/cli"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/storage"
)

// runInitCmd creates a fresh root command, sets args/stdin, captures output.
func runInitCmd(t *testing.T, stdin string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	return runCmdWithStdin(t, stdin, args...)
}

// runCmdWithStdin is like runCmd but also sets stdin.
func runCmdWithStdin(t *testing.T, stdin string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	import_bytes_buf := &bytesBuffer{}
	errBuf := &bytesBuffer{}
	root := cli.NewRootCommand()
	root.SetOut(import_bytes_buf)
	root.SetErr(errBuf)
	root.SetArgs(args)
	if stdin != "" {
		root.SetIn(strings.NewReader(stdin))
	}
	execErr := root.Execute()
	return import_bytes_buf.String(), errBuf.String(), execErr
}

// bytesBuffer is a simple io.ReadWriter backed by a strings.Builder.
type bytesBuffer struct {
	sb strings.Builder
}

func (b *bytesBuffer) Write(p []byte) (int, error) { return b.sb.Write(p) }
func (b *bytesBuffer) String() string              { return b.sb.String() }

// isolatedEnv sets up an isolated XDG environment and returns (repoRoot, xdgDir).
func isolatedEnv(t *testing.T) (repoRoot, xdgDir string) {
	t.Helper()
	dir := t.TempDir()
	repoRoot = filepath.Join(dir, "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	xdgDir = filepath.Join(dir, "xdg")
	if err := os.MkdirAll(xdgDir, 0o755); err != nil {
		t.Fatalf("mkdir xdg: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	t.Setenv("DTREE_AS", "")
	t.Setenv("DTREE_TREE", "")
	// Point git to an isolated config so user.name/email don't interfere.
	gitCfg := filepath.Join(dir, "gitconfig")
	if err := os.WriteFile(gitCfg, []byte("[user]\n\tname = TestUser\n\temail = test@example.com\n"), 0o644); err != nil {
		t.Fatalf("write gitconfig: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", gitCfg)
	return repoRoot, xdgDir
}

// TestInitCreatesStructure verifies that basic directory and file structure is created.
func TestInitCreatesStructure(t *testing.T) {
	repoRoot, _ := isolatedEnv(t)

	_, _, err := runInitCmd(t, "",
		"--repo-root", repoRoot,
		"init",
		"--non-interactive",
		"--actor-handle", "cam",
		"--actor-name", "Cam",
		"--actor-email", "cam@x.com",
	)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}

	decisionsDir := filepath.Join(repoRoot, ".decisions")
	for _, rel := range []string{
		"",
		"audit",
		".gitattributes",
		storage.ActorsFileName,
		storage.TreesFileName,
		storage.ConfigFileName,
		".index.db",
	} {
		path := filepath.Join(decisionsDir, rel)
		if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
			t.Errorf("expected %s to exist, but it does not", path)
		}
	}
}

// TestInitWritesGitattributes verifies the exact content of .gitattributes.
func TestInitWritesGitattributes(t *testing.T) {
	repoRoot, _ := isolatedEnv(t)

	_, _, err := runInitCmd(t, "",
		"--repo-root", repoRoot,
		"init",
		"--non-interactive",
		"--actor-handle", "cam",
	)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}

	gaPath := filepath.Join(repoRoot, ".decisions", ".gitattributes")
	data, readErr := os.ReadFile(gaPath)
	if readErr != nil {
		t.Fatalf("read .gitattributes: %v", readErr)
	}
	want := "audit/**/*.jsonl merge=union\n"
	if string(data) != want {
		t.Errorf(".gitattributes: got %q, want %q", string(data), want)
	}
}

// TestInitCreatesActor verifies actors.yaml content and index row.
func TestInitCreatesActor(t *testing.T) {
	repoRoot, _ := isolatedEnv(t)

	_, _, err := runInitCmd(t, "",
		"--repo-root", repoRoot,
		"init",
		"--non-interactive",
		"--actor-handle", "cam",
		"--actor-name", "Cameron",
		"--actor-email", "cam@example.com",
		"--actor-kind", "human",
	)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Verify actors.yaml.
	actorsPath := filepath.Join(repoRoot, ".decisions", storage.ActorsFileName)
	af, readErr := storage.ReadActors(actorsPath)
	if readErr != nil {
		t.Fatalf("ReadActors: %v", readErr)
	}
	if len(af.Actors) != 1 {
		t.Fatalf("expected 1 actor, got %d", len(af.Actors))
	}
	a := af.Actors[0]
	if a.Handle != "cam" {
		t.Errorf("actor handle: got %q, want %q", a.Handle, "cam")
	}
	if a.Name != "Cameron" {
		t.Errorf("actor name: got %q, want %q", a.Name, "Cameron")
	}
	if a.Email != "cam@example.com" {
		t.Errorf("actor email: got %q, want %q", a.Email, "cam@example.com")
	}
	if a.Kind != core.ActorHuman {
		t.Errorf("actor kind: got %q, want %q", a.Kind, core.ActorHuman)
	}
	if !a.Active {
		t.Error("actor should be active")
	}

	// Verify index row.
	db, dbErr := index.Open(filepath.Join(repoRoot, ".decisions", ".index.db"))
	if dbErr != nil {
		t.Fatalf("open index: %v", dbErr)
	}
	defer db.Close()

	var handle, name, email, kind string
	var active int
	row := db.Conn().QueryRow(`SELECT handle, name, email, kind, active FROM actors WHERE handle = ?`, "cam")
	if scanErr := row.Scan(&handle, &name, &email, &kind, &active); scanErr != nil {
		t.Fatalf("index actor row: %v", scanErr)
	}
	if handle != "cam" || name != "Cameron" || email != "cam@example.com" || kind != "human" || active != 1 {
		t.Errorf("index actor: got handle=%q name=%q email=%q kind=%q active=%d",
			handle, name, email, kind, active)
	}
}

// TestInitWithFirstTree verifies tree directory structure, tree.yaml, trees.yaml, and index row.
func TestInitWithFirstTree(t *testing.T) {
	repoRoot, _ := isolatedEnv(t)

	_, _, err := runInitCmd(t, "",
		"--repo-root", repoRoot,
		"init",
		"--non-interactive",
		"--actor-handle", "cam",
		"--actor-name", "Cam",
		"--actor-email", "cam@x.com",
		"--first-tree", "backend",
	)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}

	decisionsDir := filepath.Join(repoRoot, ".decisions")

	// Tree subdirectories.
	for _, rel := range []string{
		filepath.Join("backend", "decisions"),
		filepath.Join("backend", "audit"),
	} {
		path := filepath.Join(decisionsDir, rel)
		if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
			t.Errorf("expected %s to exist", path)
		}
	}

	// tree.yaml.
	treeMeta, readErr := storage.ReadTree(filepath.Join(decisionsDir, "backend", storage.TreeMetaFileName))
	if readErr != nil {
		t.Fatalf("ReadTree: %v", readErr)
	}
	if treeMeta.Slug != "backend" {
		t.Errorf("tree slug: got %q, want %q", treeMeta.Slug, "backend")
	}
	if treeMeta.CreatedAt.IsZero() {
		t.Error("tree created_at should not be zero")
	}

	// trees.yaml lists the slug.
	tf, readErr2 := storage.ReadTrees(filepath.Join(decisionsDir, storage.TreesFileName))
	if readErr2 != nil {
		t.Fatalf("ReadTrees: %v", readErr2)
	}
	if len(tf.Trees) != 1 || tf.Trees[0] != "backend" {
		t.Errorf("trees.yaml: got %v, want [backend]", tf.Trees)
	}

	// Index has tree row.
	db, dbErr := index.Open(filepath.Join(decisionsDir, ".index.db"))
	if dbErr != nil {
		t.Fatalf("open index: %v", dbErr)
	}
	defer db.Close()

	var slug string
	row := db.Conn().QueryRow(`SELECT slug FROM trees WHERE slug = ?`, "backend")
	if scanErr := row.Scan(&slug); scanErr != nil {
		t.Fatalf("index tree row: %v", scanErr)
	}
	if slug != "backend" {
		t.Errorf("index tree slug: got %q, want %q", slug, "backend")
	}
}

// TestInitTwiceIdempotent verifies that a second invocation exits 0 and prints the message.
func TestInitTwiceIdempotent(t *testing.T) {
	repoRoot, _ := isolatedEnv(t)

	// First init.
	_, _, err := runInitCmd(t, "",
		"--repo-root", repoRoot,
		"init",
		"--non-interactive",
		"--actor-handle", "cam",
	)
	if err != nil {
		t.Fatalf("first init failed: %v", err)
	}

	// Second init — should exit 0 and print "already initialized".
	out, _, err2 := runInitCmd(t, "",
		"--repo-root", repoRoot,
		"init",
		"--non-interactive",
		"--actor-handle", "cam",
	)
	if err2 != nil {
		t.Fatalf("second init returned error: %v", err2)
	}
	if !strings.Contains(out, "already initialized") {
		t.Errorf("expected 'already initialized' in output, got: %q", out)
	}
}

// TestInitNonInteractiveRequiresHandle verifies the error when handle is missing.
func TestInitNonInteractiveRequiresHandle(t *testing.T) {
	repoRoot, _ := isolatedEnv(t)

	_, _, err := runInitCmd(t, "",
		"--repo-root", repoRoot,
		"init",
		"--non-interactive",
	)
	if err == nil {
		t.Fatal("expected error when --non-interactive and no --actor-handle, got nil")
	}
	if !strings.Contains(err.Error(), "--actor-handle") {
		t.Errorf("error should mention --actor-handle, got: %v", err)
	}
}

// TestInitFromGitConfig verifies that interactive prompts pick up git config defaults.
func TestInitFromGitConfig(t *testing.T) {
	dir := t.TempDir()
	repoRoot := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	xdgDir := filepath.Join(dir, "xdg")
	if err := os.MkdirAll(xdgDir, 0o755); err != nil {
		t.Fatalf("mkdir xdg: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	t.Setenv("DTREE_AS", "")
	t.Setenv("DTREE_TREE", "")

	// Write a fake git config that provides user.name and user.email.
	gitCfg := filepath.Join(dir, "gitconfig")
	if err := os.WriteFile(gitCfg, []byte("[user]\n\tname = Git User\n\temail = gituser@example.com\n"), 0o644); err != nil {
		t.Fatalf("write gitconfig: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", gitCfg)

	// Interactive stdin: accept suggested handle from git email local part,
	// then accept all defaults.
	stdin := "\n\n\n\n" // four Enter presses: handle, name, email, kind all default

	_, _, err := runInitCmd(t, stdin,
		"--repo-root", repoRoot,
		"init",
	)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}

	actorsPath := filepath.Join(repoRoot, ".decisions", storage.ActorsFileName)
	af, readErr := storage.ReadActors(actorsPath)
	if readErr != nil {
		t.Fatalf("ReadActors: %v", readErr)
	}
	if len(af.Actors) != 1 {
		t.Fatalf("expected 1 actor, got %d", len(af.Actors))
	}
	a := af.Actors[0]
	// The suggested handle from email gituser@example.com is "gituser".
	if a.Handle != "gituser" {
		t.Errorf("actor handle from git email: got %q, want %q", a.Handle, "gituser")
	}
	if a.Name != "Git User" {
		t.Errorf("actor name from git config: got %q, want %q", a.Name, "Git User")
	}
	if a.Email != "gituser@example.com" {
		t.Errorf("actor email from git config: got %q, want %q", a.Email, "gituser@example.com")
	}
}

// TestInitEmitsAuditEvents verifies actor_add (and tree_create) events in the JSONL audit.
func TestInitEmitsAuditEvents(t *testing.T) {
	repoRoot, _ := isolatedEnv(t)

	_, _, err := runInitCmd(t, "",
		"--repo-root", repoRoot,
		"init",
		"--non-interactive",
		"--actor-handle", "cam",
		"--actor-name", "Cam",
		"--actor-email", "cam@x.com",
		"--first-tree", "backend",
	)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Read all repo-level audit events.
	events, readErr := audit.Read(repoRoot, audit.Filter{})
	if readErr != nil {
		t.Fatalf("audit.Read: %v", readErr)
	}

	var hasActorAdd, hasTreeCreate bool
	for _, ev := range events {
		switch ev.Action {
		case core.ActionActorAdd:
			hasActorAdd = true
			if ev.ID != "cam" {
				t.Errorf("actor_add event ID: got %q, want %q", ev.ID, "cam")
			}
		case core.ActionTreeCreate:
			if ev.Tree == "" {
				// repo-level tree_create event
				hasTreeCreate = true
				if ev.ID != "backend" {
					t.Errorf("tree_create event ID: got %q, want %q", ev.ID, "backend")
				}
			}
		}
	}

	if !hasActorAdd {
		t.Error("expected actor_add audit event, none found")
	}
	if !hasTreeCreate {
		t.Error("expected tree_create audit event, none found")
	}

	// Also verify tree-level audit has tree_create.
	treeEvents, readErr2 := audit.Read(repoRoot, audit.Filter{Tree: "backend"})
	if readErr2 != nil {
		t.Fatalf("audit.Read tree: %v", readErr2)
	}
	var hasTreeLevelCreate bool
	for _, ev := range treeEvents {
		if ev.Action == core.ActionTreeCreate && ev.Tree == "backend" {
			hasTreeLevelCreate = true
		}
	}
	if !hasTreeLevelCreate {
		t.Error("expected tree-level tree_create audit event, none found")
	}
}
