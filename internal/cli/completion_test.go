package cli_test

import (
	"strings"
	"testing"
)

func TestCompletionBash(t *testing.T) {
	out, _, err := runCmd(t, "completion", "bash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "bash completion") {
		t.Errorf("expected output to contain 'bash completion', got:\n%s", out)
	}
	if !strings.Contains(out, "dtree") {
		t.Errorf("expected output to contain 'dtree', got:\n%s", out)
	}
}

func TestCompletionZsh(t *testing.T) {
	out, _, err := runCmd(t, "completion", "zsh")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "zsh completion") {
		t.Errorf("expected output to contain 'zsh completion', got:\n%s", out)
	}
	if !strings.Contains(out, "dtree") {
		t.Errorf("expected output to contain 'dtree', got:\n%s", out)
	}
}

func TestCompletionFish(t *testing.T) {
	out, _, err := runCmd(t, "completion", "fish")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "fish") {
		t.Errorf("expected output to contain 'fish', got:\n%s", out)
	}
}
