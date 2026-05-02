package cli_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cgould/dtree/internal/config"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/storage"
	"gopkg.in/yaml.v3"
)

// TestWhoamiHuman verifies the human-readable output when identity is set.
func TestWhoamiHuman(t *testing.T) {
	repoRoot, xdgDir := setupTestEnv(t)

	globalPath := filepath.Join(xdgDir, "dtree", "config.yaml")
	f := &config.File{Identity: config.IdentityConfig{Default: "cam"}}
	if err := config.WriteFile(globalPath, f); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	out, _, err := runCmd(t, "--repo-root", repoRoot, "--output", "human", "whoami")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "cam") {
		t.Errorf("expected output to contain 'cam', got: %q", out)
	}
	if !strings.Contains(out, "(from") {
		t.Errorf("expected output to contain '(from', got: %q", out)
	}
}

// TestWhoamiNoIdentity verifies exit 1 and helpful message when no identity is configured.
func TestWhoamiNoIdentity(t *testing.T) {
	repoRoot, _ := setupTestEnv(t)

	_, errOut, err := runCmd(t, "--repo-root", repoRoot, "--output", "human", "whoami")
	if err == nil {
		t.Fatal("expected error when no identity configured, got nil")
	}
	if !strings.Contains(errOut, "No identity configured") {
		t.Errorf("expected stderr to mention 'No identity configured', got: %q", errOut)
	}
	if !strings.Contains(errOut, "config set") {
		t.Errorf("expected stderr to mention 'config set', got: %q", errOut)
	}
}

// TestWhoamiNotInProject verifies that a resolved identity not in actors.yaml
// produces human output with a warning and still exits 0.
func TestWhoamiNotInProject(t *testing.T) {
	repoRoot, xdgDir := setupTestEnv(t)

	globalPath := filepath.Join(xdgDir, "dtree", "config.yaml")
	f := &config.File{Identity: config.IdentityConfig{Default: "ghost"}}
	if err := config.WriteFile(globalPath, f); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// No actors.yaml written → InProject=false.
	out, _, err := runCmd(t, "--repo-root", repoRoot, "--output", "human", "whoami")
	if err != nil {
		t.Fatalf("unexpected error (want exit 0): %v", err)
	}
	if !strings.Contains(out, "ghost") {
		t.Errorf("expected output to contain 'ghost', got: %q", out)
	}
	if !strings.Contains(out, "not registered in this project") {
		t.Errorf("expected warning about unregistered actor, got: %q", out)
	}
	if !strings.Contains(out, "dtree actor add ghost") {
		t.Errorf("expected hint to run 'dtree actor add ghost', got: %q", out)
	}
}

// TestWhoamiJSON verifies --output json produces valid JSON with the right fields.
func TestWhoamiJSON(t *testing.T) {
	repoRoot, xdgDir := setupTestEnv(t)

	// Write global config with identity.
	globalPath := filepath.Join(xdgDir, "dtree", "config.yaml")
	f := &config.File{Identity: config.IdentityConfig{Default: "cam"}}
	if err := config.WriteFile(globalPath, f); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Register the actor in actors.yaml.
	actorsPath := filepath.Join(repoRoot, ".decisions", storage.ActorsFileName)
	af := &storage.ActorsFile{Actors: []core.Actor{
		{Handle: "cam", Name: "Cam", Email: "cam@x.com", Kind: core.ActorHuman, Active: true},
	}}
	if err := storage.WriteActors(actorsPath, af); err != nil {
		t.Fatalf("WriteActors: %v", err)
	}

	out, _, err := runCmd(t, "--repo-root", repoRoot, "--output", "json", "whoami")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed struct {
		Handle    string `json:"handle"`
		Source    string `json:"source"`
		InProject bool   `json:"in_project"`
		Actor     *struct {
			Name  string `json:"name"`
			Email string `json:"email"`
			Kind  string `json:"kind"`
		} `json:"actor"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid JSON output: %v\noutput:\n%s", err, out)
	}
	if parsed.Handle != "cam" {
		t.Errorf("handle: got %q, want %q", parsed.Handle, "cam")
	}
	if parsed.Source != "global" {
		t.Errorf("source: got %q, want %q", parsed.Source, "global")
	}
	if !parsed.InProject {
		t.Errorf("in_project: got false, want true")
	}
	if parsed.Actor == nil {
		t.Fatal("expected actor field in JSON output")
	}
	if parsed.Actor.Name != "Cam" {
		t.Errorf("actor.name: got %q, want %q", parsed.Actor.Name, "Cam")
	}
}

// TestWhoamiYAML verifies --output yaml produces valid YAML with the right fields.
func TestWhoamiYAML(t *testing.T) {
	repoRoot, xdgDir := setupTestEnv(t)

	globalPath := filepath.Join(xdgDir, "dtree", "config.yaml")
	f := &config.File{Identity: config.IdentityConfig{Default: "cam"}}
	if err := config.WriteFile(globalPath, f); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	out, _, err := runCmd(t, "--repo-root", repoRoot, "--output", "yaml", "whoami")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed struct {
		Handle    string `yaml:"handle"`
		Source    string `yaml:"source"`
		InProject bool   `yaml:"in_project"`
	}
	if err := yaml.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid YAML output: %v\noutput:\n%s", err, out)
	}
	if parsed.Handle != "cam" {
		t.Errorf("handle: got %q, want %q", parsed.Handle, "cam")
	}
	if parsed.Source != "global" {
		t.Errorf("source: got %q, want %q", parsed.Source, "global")
	}
}

// TestWhoamiAsFlag verifies --as <handle> overrides config identity.
func TestWhoamiAsFlag(t *testing.T) {
	repoRoot, xdgDir := setupTestEnv(t)

	// Set a different identity in global config.
	globalPath := filepath.Join(xdgDir, "dtree", "config.yaml")
	f := &config.File{Identity: config.IdentityConfig{Default: "cam"}}
	if err := config.WriteFile(globalPath, f); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Override with --as foo.
	out, _, err := runCmd(t, "--repo-root", repoRoot, "--output", "human", "whoami", "--as", "foo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "foo") {
		t.Errorf("expected output to contain 'foo' (the --as override), got: %q", out)
	}
	if strings.Contains(out, "cam") {
		t.Errorf("expected --as to override 'cam', but 'cam' appeared in output: %q", out)
	}
	if !strings.Contains(out, "flag") {
		t.Errorf("expected source to be 'flag', got: %q", out)
	}
}
