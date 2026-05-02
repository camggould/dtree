package cli_test

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestVersionHuman(t *testing.T) {
	out, _, err := runCmd(t, "version")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	checks := []string{
		"dtree ",
		"commit:",
		"built:",
		"core schema:",
		"index schema:",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestVersionJSON(t *testing.T) {
	out, _, err := runCmd(t, "version", "--output", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed struct {
		Version string `json:"version"`
		Commit  string `json:"commit"`
		Built   string `json:"built"`
		Schema  struct {
			Core  int `json:"core"`
			Index int `json:"index"`
		} `json:"schema"`
	}
	if jsonErr := json.Unmarshal([]byte(out), &parsed); jsonErr != nil {
		t.Fatalf("invalid JSON output: %v\noutput:\n%s", jsonErr, out)
	}
	if parsed.Version == "" {
		t.Error("expected non-empty version in JSON output")
	}
	if parsed.Schema.Core != 1 {
		t.Errorf("schema.core: got %d, want 1", parsed.Schema.Core)
	}
	if parsed.Schema.Index != 1 {
		t.Errorf("schema.index: got %d, want 1", parsed.Schema.Index)
	}
}

func TestVersionYAML(t *testing.T) {
	out, _, err := runCmd(t, "version", "--output", "yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed struct {
		Version string `yaml:"version"`
		Commit  string `yaml:"commit"`
		Built   string `yaml:"built"`
		Schema  struct {
			Core  int `yaml:"core"`
			Index int `yaml:"index"`
		} `yaml:"schema"`
	}
	if yamlErr := yaml.Unmarshal([]byte(out), &parsed); yamlErr != nil {
		t.Fatalf("invalid YAML output: %v\noutput:\n%s", yamlErr, out)
	}
	if parsed.Version == "" {
		t.Error("expected non-empty version in YAML output")
	}
	if parsed.Schema.Core != 1 {
		t.Errorf("schema.core: got %d, want 1", parsed.Schema.Core)
	}
	if parsed.Schema.Index != 1 {
		t.Errorf("schema.index: got %d, want 1", parsed.Schema.Index)
	}
}
