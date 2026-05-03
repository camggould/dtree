package cli_test

import (
	"strings"
	"testing"
)

func TestQueueSpearheadHappyPath(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	blocker := makeProposed(t, repoRoot, "Blocker decision")
	a := makeProposed(t, repoRoot, "A waits on blocker")
	b := makeProposed(t, repoRoot, "B waits on blocker")

	for _, dep := range []string{a, b} {
		if _, _, err := runCmdWithStdin(t, "", "--repo-root", repoRoot,
			"relate", blocker, "blocks", dep); err != nil {
			t.Fatalf("seed relate: %v", err)
		}
	}

	stdout, _, err := runCmdWithStdin(t, "", "--repo-root", repoRoot,
		"queue", "spearhead")
	if err != nil {
		t.Fatalf("queue spearhead: %v", err)
	}
	if !strings.Contains(stdout, blocker[:8]) && !strings.Contains(stdout, blocker) {
		t.Errorf("blocker %s missing from spearhead output: %q", blocker, stdout)
	}
}

func TestQueueQuickWinsHappyPath(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	makeProposed(t, repoRoot, "Some quick win")

	stdout, _, err := runCmdWithStdin(t, "", "--repo-root", repoRoot,
		"queue", "quick-wins")
	if err != nil {
		t.Fatalf("queue quick-wins: %v", err)
	}
	_ = stdout
}
