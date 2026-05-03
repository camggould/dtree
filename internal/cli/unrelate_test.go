package cli_test

import (
	"strings"
	"testing"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/core"
)

func TestUnrelateRemovesRelationship(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	src := makeDecision(t, repoRoot, "backend", "Decision A")
	tgt := makeDecision(t, repoRoot, "backend", "Decision B")

	if _, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"relate", src, "blocks", tgt,
	); err != nil {
		t.Fatalf("relate: %v", err)
	}

	out, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"unrelate", src, "blocks", tgt,
	)
	if err != nil {
		t.Fatalf("unrelate: %v", err)
	}
	if !strings.Contains(out, "Removed") {
		t.Errorf("expected Removed in output, got %q", out)
	}

	d := readDecisionByID(t, repoRoot, "backend", src)
	if len(d.Relationships) != 0 {
		t.Errorf("expected 0 relationships after unrelate, got %d", len(d.Relationships))
	}

	events, err := audit.Read(repoRoot, audit.Filter{Action: core.ActionUnrelate})
	if err != nil {
		t.Fatalf("audit.Read: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 unrelate event, got %d", len(events))
	}
	ev := events[0]
	if ev.Payload.Extra == nil {
		t.Fatal("event Extra is nil")
	}
	if got, _ := ev.Payload.Extra["target"].(string); got != tgt {
		t.Errorf("event extra target: got %q, want %q", got, tgt)
	}
}

func TestUnrelateErrorsWhenMissing(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	src := makeDecision(t, repoRoot, "backend", "Decision A")
	tgt := makeDecision(t, repoRoot, "backend", "Decision B")

	_, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"unrelate", src, "blocks", tgt,
	)
	if err == nil {
		t.Fatal("expected error for missing relationship, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got %v", err)
	}
}

func TestUnrelateRefusesSupersedes(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	src := makeDecision(t, repoRoot, "backend", "Decision A")
	tgt := makeDecision(t, repoRoot, "backend", "Decision B")

	for _, typ := range []string{"supersedes", "superseded_by"} {
		_, _, err := runCmdWithStdin(t, "",
			"--repo-root", repoRoot,
			"unrelate", src, typ, tgt,
		)
		if err == nil {
			t.Errorf("expected error for type %q, got nil", typ)
			continue
		}
		want := "must be removed via 'dtree supersede'"
		if !strings.Contains(err.Error(), want) {
			t.Errorf("type %q: expected %q in error, got %v", typ, want, err)
		}
	}
}

func TestUnrelateHonorsAsFlag(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	src := makeDecision(t, repoRoot, "backend", "Decision A")
	tgt := makeDecision(t, repoRoot, "backend", "Decision B")

	if _, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"actor", "add", "bob", "--name", "Bob", "--email", "bob@example.com",
	); err != nil {
		t.Fatalf("actor add bob: %v", err)
	}
	if _, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"relate", src, "influences", tgt,
	); err != nil {
		t.Fatalf("relate: %v", err)
	}

	if _, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"unrelate", src, "influences", tgt,
		"--as", "bob",
	); err != nil {
		t.Fatalf("unrelate --as bob: %v", err)
	}

	events, err := audit.Read(repoRoot, audit.Filter{Action: core.ActionUnrelate, Actor: "bob"})
	if err != nil {
		t.Fatalf("audit.Read: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 unrelate event by bob, got %d", len(events))
	}
}
