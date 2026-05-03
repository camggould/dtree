package cli_test

import (
	"strings"
	"testing"

	"github.com/cgould/dtree/internal/core"
)

func TestEditFieldUpdatesSummary(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := makeProposed(t, repoRoot, "Original summary")

	if _, _, err := runCmdWithStdin(t, "", "--repo-root", repoRoot,
		"edit", id,
		"--field", "summary=New summary",
	); err != nil {
		t.Fatalf("edit: %v", err)
	}

	d := loadDecisionFile(t, repoRoot, "backend", id)
	if d.Summary != "New summary" {
		t.Errorf("summary: got %q, want %q", d.Summary, "New summary")
	}
}

func TestEditFieldRejectsUnknown(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := makeProposed(t, repoRoot, "x")

	_, _, err := runCmdWithStdin(t, "", "--repo-root", repoRoot,
		"edit", id,
		"--field", "bogus=value",
	)
	if err == nil {
		t.Fatal("expected error on unknown field")
	}
}

func TestEditPriority(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := makeProposed(t, repoRoot, "x")

	if _, _, err := runCmdWithStdin(t, "", "--repo-root", repoRoot,
		"edit", id,
		"--field", "priority=high",
	); err != nil {
		t.Fatalf("edit priority: %v", err)
	}

	d := loadDecisionFile(t, repoRoot, "backend", id)
	if d.Priority != core.PriorityHigh {
		t.Errorf("priority: got %q, want %q", d.Priority, core.PriorityHigh)
	}
}

var _ = strings.Contains
