package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cgould/dtree/internal/config"
	"github.com/cgould/dtree/internal/core"
)

// TestReadMissingFile verifies that a non-existent file returns (nil, nil).
func TestReadMissingFile(t *testing.T) {
	dir := t.TempDir()
	f, err := config.ReadFile(filepath.Join(dir, "no-such-file.yaml"))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if f != nil {
		t.Fatalf("expected nil file, got %+v", f)
	}
}

// TestWriteThenRead round-trips a file with an identity catalog.
func TestWriteThenRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	original := &config.File{
		SchemaVersion: core.SchemaVersion,
		Identity: config.IdentityConfig{
			Default: "cam",
			Identities: []core.Actor{
				{Handle: "cam", Name: "Cameron Gould", Email: "cam@example.com", Kind: core.ActorHuman, Active: true},
				{Handle: "cam-claude", Kind: core.ActorAgent},
			},
		},
		Editor: "nvim",
		Output: "json",
		Color:  "always",
	}

	if err := config.WriteFile(path, original); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := config.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil file")
	}
	if got.Identity.Default != original.Identity.Default {
		t.Errorf("Identity.Default: got %q, want %q", got.Identity.Default, original.Identity.Default)
	}
	if len(got.Identity.Identities) != 2 {
		t.Fatalf("expected 2 identities, got %d", len(got.Identity.Identities))
	}
	if got.Identity.Identities[0].Handle != "cam" {
		t.Errorf("first identity handle: got %q, want %q", got.Identity.Identities[0].Handle, "cam")
	}
	if got.Identity.Identities[1].Handle != "cam-claude" {
		t.Errorf("second identity handle: got %q, want %q", got.Identity.Identities[1].Handle, "cam-claude")
	}
	if got.Editor != original.Editor {
		t.Errorf("Editor: got %q, want %q", got.Editor, original.Editor)
	}
	if got.Output != original.Output {
		t.Errorf("Output: got %q, want %q", got.Output, original.Output)
	}
	if got.Color != original.Color {
		t.Errorf("Color: got %q, want %q", got.Color, original.Color)
	}
}

// TestLocalPath verifies the local config path construction.
func TestLocalPath(t *testing.T) {
	root := "/some/repo"
	want := "/some/repo/.decisions/config.yaml"
	got := config.LocalPath(root)
	if got != want {
		t.Errorf("LocalPath: got %q, want %q", got, want)
	}
}

// TestGlobalPathRespectsXDG verifies XDG_CONFIG_HOME is honored.
func TestGlobalPathRespectsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/foo")
	got, err := config.GlobalPath()
	if err != nil {
		t.Fatalf("GlobalPath: %v", err)
	}
	want := "/tmp/foo/dtree/config.yaml"
	if got != want {
		t.Errorf("GlobalPath: got %q, want %q", got, want)
	}
}

// TestLoadEmpty verifies defaults when no config files exist and no env vars.
func TestLoadEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	t.Setenv("DTREE_AS", "")
	t.Setenv("DTREE_TREE", "")
	t.Setenv("EDITOR", "")

	r, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if r.Identity != "" {
		t.Errorf("Identity: got %q, want empty", r.Identity)
	}
	if r.IdentitySrc != config.SourceDefault {
		t.Errorf("IdentitySrc: got %q, want %q", r.IdentitySrc, config.SourceDefault)
	}
	if r.Editor != "vi" {
		t.Errorf("Editor: got %q, want %q", r.Editor, "vi")
	}
	if r.EditorSrc != config.SourceDefault {
		t.Errorf("EditorSrc: got %q, want %q", r.EditorSrc, config.SourceDefault)
	}
	if r.Output != "human" {
		t.Errorf("Output: got %q, want %q", r.Output, "human")
	}
	if r.OutputSrc != config.SourceDefault {
		t.Errorf("OutputSrc: got %q, want %q", r.OutputSrc, config.SourceDefault)
	}
	if r.Color != "auto" {
		t.Errorf("Color: got %q, want %q", r.Color, "auto")
	}
	if r.ColorSrc != config.SourceDefault {
		t.Errorf("ColorSrc: got %q, want %q", r.ColorSrc, config.SourceDefault)
	}
}

// TestLoadGlobalOnly verifies global identity.default is picked up.
func TestLoadGlobalOnly(t *testing.T) {
	dir := t.TempDir()
	xdg := filepath.Join(dir, "xdg")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("DTREE_AS", "")
	t.Setenv("DTREE_TREE", "")

	globalCfg := &config.File{
		Identity: config.IdentityConfig{Default: "alice"},
	}
	globalPath := filepath.Join(xdg, "dtree", "config.yaml")
	if err := config.WriteFile(globalPath, globalCfg); err != nil {
		t.Fatalf("WriteFile global: %v", err)
	}

	r, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if r.Identity != "alice" {
		t.Errorf("Identity: got %q, want %q", r.Identity, "alice")
	}
	if r.IdentitySrc != config.SourceGlobal {
		t.Errorf("IdentitySrc: got %q, want %q", r.IdentitySrc, config.SourceGlobal)
	}
}

// TestLoadLocalOverridesGlobal verifies local config takes precedence over global.
func TestLoadLocalOverridesGlobal(t *testing.T) {
	dir := t.TempDir()
	xdg := filepath.Join(dir, "xdg")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("DTREE_AS", "")
	t.Setenv("DTREE_TREE", "")

	globalCfg := &config.File{Identity: config.IdentityConfig{Default: "global-user"}}
	if err := config.WriteFile(filepath.Join(xdg, "dtree", "config.yaml"), globalCfg); err != nil {
		t.Fatalf("WriteFile global: %v", err)
	}

	repoRoot := filepath.Join(dir, "repo")
	localCfg := &config.File{Identity: config.IdentityConfig{Default: "local-user"}}
	if err := config.WriteFile(config.LocalPath(repoRoot), localCfg); err != nil {
		t.Fatalf("WriteFile local: %v", err)
	}

	r, err := config.Load(repoRoot)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if r.Identity != "local-user" {
		t.Errorf("Identity: got %q, want %q", r.Identity, "local-user")
	}
	if r.IdentitySrc != config.SourceLocal {
		t.Errorf("IdentitySrc: got %q, want %q", r.IdentitySrc, config.SourceLocal)
	}
}

// TestLoadEnvOverridesLocal verifies DTREE_AS overrides the local config.
func TestLoadEnvOverridesLocal(t *testing.T) {
	dir := t.TempDir()
	xdg := filepath.Join(dir, "xdg")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("DTREE_AS", "env-user")
	t.Setenv("DTREE_TREE", "")

	repoRoot := filepath.Join(dir, "repo")
	localCfg := &config.File{Identity: config.IdentityConfig{Default: "local-user"}}
	if err := config.WriteFile(config.LocalPath(repoRoot), localCfg); err != nil {
		t.Fatalf("WriteFile local: %v", err)
	}

	r, err := config.Load(repoRoot)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if r.Identity != "env-user" {
		t.Errorf("Identity: got %q, want %q", r.Identity, "env-user")
	}
	if r.IdentitySrc != config.SourceEnv {
		t.Errorf("IdentitySrc: got %q, want %q", r.IdentitySrc, config.SourceEnv)
	}
}

// TestLoadEnvDtreeTreeOverridesLocal verifies DTREE_TREE overrides default_tree.
func TestLoadEnvDtreeTreeOverridesLocal(t *testing.T) {
	dir := t.TempDir()
	xdg := filepath.Join(dir, "xdg")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("DTREE_AS", "")
	t.Setenv("DTREE_TREE", "env-tree")

	repoRoot := filepath.Join(dir, "repo")
	localCfg := &config.File{DefaultTree: "local-tree"}
	if err := config.WriteFile(config.LocalPath(repoRoot), localCfg); err != nil {
		t.Fatalf("WriteFile local: %v", err)
	}

	r, err := config.Load(repoRoot)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if r.DefaultTree != "env-tree" {
		t.Errorf("DefaultTree: got %q, want %q", r.DefaultTree, "env-tree")
	}
	if r.DefaultTreeSrc != config.SourceEnv {
		t.Errorf("DefaultTreeSrc: got %q, want %q", r.DefaultTreeSrc, config.SourceEnv)
	}
}

// TestEditorDefaultsToEnvEditor verifies $EDITOR is used when set.
func TestEditorDefaultsToEnvEditor(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	t.Setenv("DTREE_AS", "")
	t.Setenv("DTREE_TREE", "")
	t.Setenv("EDITOR", "emacs")

	r, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if r.Editor != "emacs" {
		t.Errorf("Editor: got %q, want %q", r.Editor, "emacs")
	}
	if r.EditorSrc != config.SourceEnv {
		t.Errorf("EditorSrc: got %q, want %q", r.EditorSrc, config.SourceEnv)
	}
}

// TestEditorDefaultsToVi verifies vi is the fallback when $EDITOR is unset.
func TestEditorDefaultsToVi(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	t.Setenv("DTREE_AS", "")
	t.Setenv("DTREE_TREE", "")
	t.Setenv("EDITOR", "")

	r, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if r.Editor != "vi" {
		t.Errorf("Editor: got %q, want %q", r.Editor, "vi")
	}
	if r.EditorSrc != config.SourceDefault {
		t.Errorf("EditorSrc: got %q, want %q", r.EditorSrc, config.SourceDefault)
	}
}

// TestSetGlobalIdentityDefault verifies Set writes to global config.
func TestSetGlobalIdentityDefault(t *testing.T) {
	dir := t.TempDir()
	xdg := filepath.Join(dir, "xdg")
	t.Setenv("XDG_CONFIG_HOME", xdg)

	if err := config.Set(config.SourceGlobal, "", "identity.default", "bob"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	globalPath := filepath.Join(xdg, "dtree", "config.yaml")
	f, err := config.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if f == nil {
		t.Fatal("expected non-nil file")
	}
	if f.Identity.Default != "bob" {
		t.Errorf("Identity.Default: got %q, want %q", f.Identity.Default, "bob")
	}
}

// TestSetLocalIdentityDefault verifies Set writes to local config.
func TestSetLocalIdentityDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))

	repoRoot := filepath.Join(dir, "repo")
	if err := config.Set(config.SourceLocal, repoRoot, "identity.default", "carol"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	f, err := config.ReadFile(config.LocalPath(repoRoot))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if f == nil {
		t.Fatal("expected non-nil file")
	}
	if f.Identity.Default != "carol" {
		t.Errorf("Identity.Default: got %q, want %q", f.Identity.Default, "carol")
	}
}

// TestSetInvalidScope verifies that SourceFlag and SourceEnv are rejected.
func TestSetInvalidScope(t *testing.T) {
	for _, scope := range []config.Source{config.SourceFlag, config.SourceEnv} {
		if err := config.Set(scope, "", "output", "json"); err == nil {
			t.Errorf("Set(%q): expected error, got nil", scope)
		}
	}
}

// TestSetInvalidValueOutput verifies that invalid output values are rejected.
func TestSetInvalidValueOutput(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	if err := config.Set(config.SourceGlobal, "", "output", "foo"); err == nil {
		t.Error("expected error for invalid output value, got nil")
	}
}

// TestSetInvalidValueColor verifies that invalid color values are rejected.
func TestSetInvalidValueColor(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	if err := config.Set(config.SourceGlobal, "", "color", "foo"); err == nil {
		t.Error("expected error for invalid color value, got nil")
	}
}

// TestUnsetClears verifies Unset removes a value.
func TestUnsetClears(t *testing.T) {
	dir := t.TempDir()
	xdg := filepath.Join(dir, "xdg")
	t.Setenv("XDG_CONFIG_HOME", xdg)

	if err := config.Set(config.SourceGlobal, "", "identity.default", "dave"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if err := config.Unset(config.SourceGlobal, "", "identity.default"); err != nil {
		t.Fatalf("Unset: %v", err)
	}

	f, err := config.ReadFile(filepath.Join(xdg, "dtree", "config.yaml"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if f != nil && f.Identity.Default != "" {
		t.Errorf("Identity.Default: got %q, want empty after unset", f.Identity.Default)
	}
}

// TestListReturnsAllKeys verifies List returns all expected keys.
func TestListReturnsAllKeys(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	t.Setenv("DTREE_AS", "")
	t.Setenv("DTREE_TREE", "")
	t.Setenv("EDITOR", "")

	m, err := config.List("")
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	expectedKeys := []string{"identity.default", "editor", "output", "color", "default_tree"}
	for _, k := range expectedKeys {
		if _, ok := m[k]; !ok {
			t.Errorf("List: missing key %q", k)
		}
	}
}

// TestGetUnknownKey verifies Get returns (_, _, false) for unknown keys.
func TestGetUnknownKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	t.Setenv("DTREE_AS", "")
	t.Setenv("DTREE_TREE", "")

	r, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	_, _, ok := r.Get("no.such.key")
	if ok {
		t.Error("Get: expected false for unknown key, got true")
	}
}

// TestIdentityCatalogReturnsGlobalList verifies IdentityCatalog returns global identities.
func TestIdentityCatalogReturnsGlobalList(t *testing.T) {
	dir := t.TempDir()
	xdg := filepath.Join(dir, "xdg")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("DTREE_AS", "")
	t.Setenv("DTREE_TREE", "")

	globalCfg := &config.File{
		Identity: config.IdentityConfig{
			Default: "alice",
			Identities: []core.Actor{
				{Handle: "alice", Kind: core.ActorHuman},
				{Handle: "bot", Kind: core.ActorAgent},
			},
		},
	}
	if err := config.WriteFile(filepath.Join(xdg, "dtree", "config.yaml"), globalCfg); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	r, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	catalog := config.IdentityCatalog(r)
	if len(catalog) != 2 {
		t.Fatalf("IdentityCatalog: got %d identities, want 2", len(catalog))
	}
	if catalog[0].Handle != "alice" {
		t.Errorf("catalog[0].Handle: got %q, want %q", catalog[0].Handle, "alice")
	}
	if catalog[1].Handle != "bot" {
		t.Errorf("catalog[1].Handle: got %q, want %q", catalog[1].Handle, "bot")
	}
}

// TestSchemaVersionDefaulted verifies that a file with schema_version=0 gets
// it set to core.SchemaVersion on read.
func TestSchemaVersionDefaulted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	// Write raw YAML with no schema_version field.
	raw := []byte("editor: nano\n")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	f, err := config.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if f == nil {
		t.Fatal("expected non-nil file")
	}
	if f.SchemaVersion != core.SchemaVersion {
		t.Errorf("SchemaVersion: got %d, want %d", f.SchemaVersion, core.SchemaVersion)
	}
}
