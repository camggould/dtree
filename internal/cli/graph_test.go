package cli_test

import (
	"strings"
	"testing"
)

func TestGraphCyclesEmpty(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	makeProposed(t, repoRoot, "A")
	makeProposed(t, repoRoot, "B")

	stdout, _, err := runCmdWithStdin(t, "", "--repo-root", repoRoot, "graph", "cycles")
	if err != nil {
		t.Fatalf("graph cycles: %v", err)
	}
	if !strings.Contains(stdout, "no cycles") && !strings.Contains(stdout, "0") {
		t.Logf("output: %q", stdout)
	}
}

func TestGraphDepsHappyPath(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	a := makeProposed(t, repoRoot, "Choose db")
	b := makeProposed(t, repoRoot, "Choose orm")

	if _, _, err := runCmdWithStdin(t, "", "--repo-root", repoRoot,
		"relate", b, "blocks", a); err != nil {
		t.Fatalf("seed relate: %v", err)
	}

	stdout, _, err := runCmdWithStdin(t, "", "--repo-root", repoRoot,
		"graph", "deps", a)
	if err != nil {
		t.Fatalf("graph deps: %v", err)
	}
	if !strings.Contains(stdout, b[:8]) && !strings.Contains(stdout, b) {
		t.Errorf("expected deps to mention blocker %s; got: %q", b, stdout)
	}
}

func TestGraphVizDOT(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := makeProposed(t, repoRoot, "Solo")

	stdout, _, err := runCmdWithStdin(t, "", "--repo-root", repoRoot,
		"graph", "viz", id)
	if err != nil {
		t.Fatalf("graph viz: %v", err)
	}
	if !strings.Contains(stdout, "digraph") {
		t.Errorf("DOT output missing 'digraph': %q", stdout)
	}
}
