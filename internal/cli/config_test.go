package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cgould/dtree/internal/cli"
	"github.com/cgould/dtree/internal/config"
	"gopkg.in/yaml.v3"
)

// setupTestEnv creates a temp directory with .decisions/ pre-created and
// sets XDG_CONFIG_HOME to an isolated directory. Returns repoRoot and xdgDir.
func setupTestEnv(t *testing.T) (repoRoot, xdgDir string) {
	t.Helper()
	dir := t.TempDir()
	repoRoot = filepath.Join(dir, "repo")
	decisionsDir := filepath.Join(repoRoot, ".decisions")
	if err := os.MkdirAll(decisionsDir, 0o755); err != nil {
		t.Fatalf("mkdir .decisions: %v", err)
	}
	xdgDir = filepath.Join(dir, "xdg")
	if err := os.MkdirAll(xdgDir, 0o755); err != nil {
		t.Fatalf("mkdir xdg: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	t.Setenv("DTREE_AS", "")
	t.Setenv("DTREE_TREE", "")
	t.Setenv("EDITOR", "")
	return repoRoot, xdgDir
}

// runCmd creates a fresh root command, sets args, and captures stdout/stderr.
// Returns (stdout, stderr, error from Execute).
func runCmd(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	root := cli.NewRootCommand()
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

// --- config get ---

func TestConfigGetUnknownKey(t *testing.T) {
	repoRoot, _ := setupTestEnv(t)

	_, errOut, err := runCmd(t, "--repo-root", repoRoot, "config", "get", "no.such.key")
	if err == nil {
		t.Fatal("expected error for unknown key, got nil")
	}
	if !strings.Contains(errOut, "unknown config key") {
		t.Errorf("expected stderr to contain 'unknown config key', got: %q", errOut)
	}
}

func TestConfigGetKnownKey(t *testing.T) {
	repoRoot, xdgDir := setupTestEnv(t)

	// Set a known value in global config.
	globalPath := filepath.Join(xdgDir, "dtree", "config.yaml")
	f := &config.File{Identity: config.IdentityConfig{Default: "testuser"}}
	if err := config.WriteFile(globalPath, f); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	out, _, err := runCmd(t, "--repo-root", repoRoot, "config", "get", "identity.default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "testuser") {
		t.Errorf("expected output to contain 'testuser', got: %q", out)
	}
	if !strings.Contains(out, "(from") {
		t.Errorf("expected output to contain '(from', got: %q", out)
	}
}

func TestConfigGetNoSourceFlag(t *testing.T) {
	repoRoot, xdgDir := setupTestEnv(t)

	globalPath := filepath.Join(xdgDir, "dtree", "config.yaml")
	f := &config.File{Identity: config.IdentityConfig{Default: "testuser"}}
	if err := config.WriteFile(globalPath, f); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	out, _, err := runCmd(t, "--repo-root", repoRoot, "config", "get", "--no-source", "identity.default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, "(from") {
		t.Errorf("expected --no-source to hide '(from ...)', got: %q", out)
	}
	if !strings.Contains(out, "testuser") {
		t.Errorf("expected output to contain 'testuser', got: %q", out)
	}
}

// --- config set ---

func TestConfigSetGlobal(t *testing.T) {
	repoRoot, xdgDir := setupTestEnv(t)

	out, _, err := runCmd(t, "--repo-root", repoRoot, "config", "set", "--global", "identity.default", "newuser")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Set identity.default = newuser in global") {
		t.Errorf("unexpected output: %q", out)
	}

	// Verify the global file was written.
	globalPath := filepath.Join(xdgDir, "dtree", "config.yaml")
	gf, readErr := config.ReadFile(globalPath)
	if readErr != nil {
		t.Fatalf("ReadFile: %v", readErr)
	}
	if gf == nil || gf.Identity.Default != "newuser" {
		t.Errorf("global Identity.Default: got %q, want %q", gf.Identity.Default, "newuser")
	}
}

func TestConfigSetLocal(t *testing.T) {
	repoRoot, _ := setupTestEnv(t)

	out, _, err := runCmd(t, "--repo-root", repoRoot, "config", "set", "--local", "identity.default", "localuser")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Set identity.default = localuser in local") {
		t.Errorf("unexpected output: %q", out)
	}

	localPath := config.LocalPath(repoRoot)
	lf, readErr := config.ReadFile(localPath)
	if readErr != nil {
		t.Fatalf("ReadFile: %v", readErr)
	}
	if lf == nil || lf.Identity.Default != "localuser" {
		t.Errorf("local Identity.Default: got %q, want %q", lf.Identity.Default, "localuser")
	}
}

func TestConfigSetLocalMissingDecisions(t *testing.T) {
	// Repo root without .decisions/ directory.
	dir := t.TempDir()
	repoRoot := filepath.Join(dir, "norepo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))

	_, errOut, err := runCmd(t, "--repo-root", repoRoot, "config", "set", "--local", "identity.default", "x")
	if err == nil {
		t.Fatal("expected error when .decisions/ missing, got nil")
	}
	if !strings.Contains(errOut, "no .decisions/ found") {
		t.Errorf("expected error about missing .decisions/, got: %q", errOut)
	}
	if !strings.Contains(errOut, "dtree init") {
		t.Errorf("expected error to mention 'dtree init', got: %q", errOut)
	}
}

func TestConfigSetRequiresScope(t *testing.T) {
	repoRoot, _ := setupTestEnv(t)

	// Neither --local nor --global.
	_, errOut, err := runCmd(t, "--repo-root", repoRoot, "config", "set", "identity.default", "x")
	if err == nil {
		t.Fatal("expected error when neither --local nor --global, got nil")
	}
	if !strings.Contains(errOut, "must specify") {
		t.Errorf("expected scope error, got stderr: %q", errOut)
	}

	// Both --local and --global.
	_, errOut2, err2 := runCmd(t, "--repo-root", repoRoot, "config", "set", "--local", "--global", "identity.default", "x")
	if err2 == nil {
		t.Fatal("expected error when both --local and --global, got nil")
	}
	if !strings.Contains(errOut2, "cannot set both") {
		t.Errorf("expected scope conflict error, got stderr: %q", errOut2)
	}
}

func TestConfigSetInvalidValue(t *testing.T) {
	repoRoot, _ := setupTestEnv(t)

	_, _, err := runCmd(t, "--repo-root", repoRoot, "config", "set", "--global", "output", "foo")
	if err == nil {
		t.Fatal("expected error for invalid output value, got nil")
	}
}

// --- config unset ---

func TestConfigUnsetGlobal(t *testing.T) {
	repoRoot, xdgDir := setupTestEnv(t)

	// First set.
	globalPath := filepath.Join(xdgDir, "dtree", "config.yaml")
	f := &config.File{Identity: config.IdentityConfig{Default: "dave"}}
	if err := config.WriteFile(globalPath, f); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Now unset.
	out, _, err := runCmd(t, "--repo-root", repoRoot, "config", "unset", "--global", "identity.default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Unset identity.default in global") {
		t.Errorf("unexpected output: %q", out)
	}

	// Verify cleared.
	gf, readErr := config.ReadFile(globalPath)
	if readErr != nil {
		t.Fatalf("ReadFile: %v", readErr)
	}
	if gf != nil && gf.Identity.Default != "" {
		t.Errorf("Identity.Default: got %q, want empty", gf.Identity.Default)
	}
}

// --- config list ---

func TestConfigList(t *testing.T) {
	repoRoot, xdgDir := setupTestEnv(t)

	globalPath := filepath.Join(xdgDir, "dtree", "config.yaml")
	f := &config.File{Identity: config.IdentityConfig{Default: "listuser"}}
	if err := config.WriteFile(globalPath, f); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	out, _, err := runCmd(t, "--repo-root", repoRoot, "config", "list", "--output", "human")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify all expected keys are present in stable sorted order.
	requiredKeys := []string{"color", "default_tree", "editor", "identity.default", "output"}
	for _, k := range requiredKeys {
		if !strings.Contains(out, k) {
			t.Errorf("expected output to contain key %q, got:\n%s", k, out)
		}
	}

	// Verify the set value appears.
	if !strings.Contains(out, "listuser") {
		t.Errorf("expected output to contain 'listuser', got:\n%s", out)
	}

	// Verify stable order: check that earlier keys appear before later ones.
	colorIdx := strings.Index(out, "color")
	editorIdx := strings.Index(out, "editor")
	identityIdx := strings.Index(out, "identity.default")
	if colorIdx >= editorIdx || editorIdx >= identityIdx {
		t.Errorf("keys not in stable sorted order: color=%d editor=%d identity.default=%d", colorIdx, editorIdx, identityIdx)
	}
}

func TestConfigListJSON(t *testing.T) {
	repoRoot, xdgDir := setupTestEnv(t)

	globalPath := filepath.Join(xdgDir, "dtree", "config.yaml")
	f := &config.File{Identity: config.IdentityConfig{Default: "jsonuser"}}
	if err := config.WriteFile(globalPath, f); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	out, _, err := runCmd(t, "--repo-root", repoRoot, "config", "list", "--output", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Must be valid JSON.
	var parsed map[string]struct {
		Value  string `json:"value"`
		Source string `json:"source"`
	}
	if jsonErr := json.Unmarshal([]byte(out), &parsed); jsonErr != nil {
		t.Fatalf("invalid JSON output: %v\noutput:\n%s", jsonErr, out)
	}

	entry, ok := parsed["identity.default"]
	if !ok {
		t.Fatal("JSON output missing 'identity.default' key")
	}
	if entry.Value != "jsonuser" {
		t.Errorf("identity.default value: got %q, want %q", entry.Value, "jsonuser")
	}
	if entry.Source != "global" {
		t.Errorf("identity.default source: got %q, want %q", entry.Source, "global")
	}
}

func TestConfigListYAML(t *testing.T) {
	repoRoot, xdgDir := setupTestEnv(t)

	globalPath := filepath.Join(xdgDir, "dtree", "config.yaml")
	f := &config.File{Identity: config.IdentityConfig{Default: "yamluser"}}
	if err := config.WriteFile(globalPath, f); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	out, _, err := runCmd(t, "--repo-root", repoRoot, "config", "list", "--output", "yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Must be valid YAML.
	var parsed map[string]struct {
		Value  string `yaml:"value"`
		Source string `yaml:"source"`
	}
	if yamlErr := yaml.Unmarshal([]byte(out), &parsed); yamlErr != nil {
		t.Fatalf("invalid YAML output: %v\noutput:\n%s", yamlErr, out)
	}

	entry, ok := parsed["identity.default"]
	if !ok {
		t.Fatal("YAML output missing 'identity.default' key")
	}
	if entry.Value != "yamluser" {
		t.Errorf("identity.default value: got %q, want %q", entry.Value, "yamluser")
	}
}

// --- config edit ---

// makeEditorScript creates a shell script that performs the given action on
// the file passed as $1, then returns exitCode. Returns the path to the script.
func makeEditorScript(t *testing.T, body string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-editor.sh")
	content := fmt.Sprintf("#!/bin/sh\n%s\nexit %d\n", body, exitCode)
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write editor script: %v", err)
	}
	return script
}

func TestConfigEditCreatesMissingFile(t *testing.T) {
	repoRoot, xdgDir := setupTestEnv(t)

	globalPath := filepath.Join(xdgDir, "dtree", "config.yaml")

	// Verify file doesn't exist yet.
	if _, err := os.Stat(globalPath); !os.IsNotExist(err) {
		t.Fatal("expected global config to not exist yet")
	}

	// Use a no-op editor so we can check the file was created before it opens.
	// The editor receives the path as $1 — it just exits 0 without modification.
	editorScript := makeEditorScript(t, "# no-op", 0)
	t.Setenv("EDITOR", editorScript)

	_, _, err := runCmd(t, "--repo-root", repoRoot, "config", "edit", "--global")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// File should exist now.
	if _, statErr := os.Stat(globalPath); os.IsNotExist(statErr) {
		t.Fatal("expected global config to be created, but it doesn't exist")
	}

	// File should be parseable.
	f, readErr := config.ReadFile(globalPath)
	if readErr != nil {
		t.Fatalf("ReadFile: %v", readErr)
	}
	if f == nil {
		t.Fatal("expected non-nil file after edit")
	}
}

func TestConfigEditNonZeroEditor(t *testing.T) {
	repoRoot, _ := setupTestEnv(t)

	editorScript := makeEditorScript(t, "# fail", 1)
	t.Setenv("EDITOR", editorScript)

	_, errOut, err := runCmd(t, "--repo-root", repoRoot, "config", "edit", "--global")
	if err == nil {
		t.Fatal("expected error when editor exits non-zero, got nil")
	}
	if !strings.Contains(errOut, "Editor exited with non-zero status") {
		t.Errorf("expected stderr to contain editor exit message, got: %q", errOut)
	}
}

func TestConfigEditInvalidYamlAfterSave(t *testing.T) {
	repoRoot, _ := setupTestEnv(t)

	// Editor writes invalid YAML to the file.
	editorScript := makeEditorScript(t, `printf 'invalid: : yaml: :\n' > "$1"`, 0)
	t.Setenv("EDITOR", editorScript)

	_, errOut, err := runCmd(t, "--repo-root", repoRoot, "config", "edit", "--global")
	if err == nil {
		t.Fatal("expected error for invalid YAML after editor, got nil")
	}
	if !strings.Contains(errOut, "error") {
		t.Errorf("expected stderr to contain error message, got: %q", errOut)
	}
}
