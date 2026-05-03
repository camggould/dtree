package cli_test

import (
	"strings"
	"testing"

	"github.com/cgould/dtree/internal/core"
)

func TestSupersedeHappyPath(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	oldID := makeProposed(t, repoRoot, "Pick old parser")
	newID := makeProposed(t, repoRoot, "Pick new parser")

	if _, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"supersede", oldID,
		"--by", newID,
	); err != nil {
		t.Fatalf("supersede: %v", err)
	}

	oldD := loadDecisionFile(t, repoRoot, "backend", oldID)
	if oldD.Status != core.StatusSuperseded {
		t.Errorf("old status: got %q, want %q", oldD.Status, core.StatusSuperseded)
	}
	if !hasSupersedesEdge(oldD, newID) {
		t.Errorf("old missing supersedes->new edge; rels=%v", oldD.Relationships)
	}

	newD := loadDecisionFile(t, repoRoot, "backend", newID)
	if !hasSupersedesEdge(newD, oldID) {
		t.Errorf("new missing reciprocal supersedes->old edge; rels=%v", newD.Relationships)
	}
}

func TestSupersedeMissingBy(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	oldID := makeProposed(t, repoRoot, "x")

	_, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"supersede", oldID,
	)
	if err == nil {
		t.Fatal("expected error when --by is missing")
	}
	if !strings.Contains(err.Error(), "--by is required") {
		t.Errorf("error: %v", err)
	}
}

func TestSupersedeSelfRefused(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := makeProposed(t, repoRoot, "Solo")

	_, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"supersede", id,
		"--by", id,
	)
	if err == nil {
		t.Fatal("expected self-supersede to be refused")
	}
	if !strings.Contains(err.Error(), "cannot supersede itself") {
		t.Errorf("error: %v", err)
	}
}

func TestSupersedeAlreadySupersededRefused(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	oldID := makeProposed(t, repoRoot, "Old")
	mid := makeProposed(t, repoRoot, "Mid")
	newID := makeProposed(t, repoRoot, "New")

	if _, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"supersede", oldID,
		"--by", mid,
	); err != nil {
		t.Fatalf("first supersede: %v", err)
	}
	_, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"supersede", oldID,
		"--by", newID,
	)
	if err == nil {
		t.Fatal("expected error superseding already-superseded decision")
	}
	if !strings.Contains(err.Error(), "already superseded") {
		t.Errorf("error: %v", err)
	}
}

func hasSupersedesEdge(d *core.Decision, target string) bool {
	for _, r := range d.Relationships {
		if r.Type == core.RelSupersedes && r.Target == target {
			return true
		}
	}
	return false
}
