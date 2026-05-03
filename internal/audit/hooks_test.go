package audit

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/cgould/dtree/internal/core"
)

func TestRegisterHookFires(t *testing.T) {
	repoRoot := t.TempDir()
	if err := initTestRepo(repoRoot, "alpha"); err != nil {
		t.Fatal(err)
	}

	var (
		mu    sync.Mutex
		fired []core.Event
	)
	unreg := RegisterHook(func(ev core.Event) {
		mu.Lock()
		fired = append(fired, ev)
		mu.Unlock()
	})
	defer unreg()

	ev := core.Event{
		Actor: "alice", Action: core.ActionCreate, Kind: core.KindDecision,
		Tree: "alpha", ID: "01HZZZZZZZZZZZZZZZZZZZZZZZ",
	}
	if err := Append(repoRoot, ev); err != nil {
		t.Fatalf("append: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(fired) != 1 {
		t.Fatalf("hook fired %d times, want 1", len(fired))
	}
	if fired[0].ID != ev.ID {
		t.Errorf("hook event id: got %q want %q", fired[0].ID, ev.ID)
	}
}

func TestUnregisterHookStopsFiring(t *testing.T) {
	repoRoot := t.TempDir()
	if err := initTestRepo(repoRoot, "alpha"); err != nil {
		t.Fatal(err)
	}

	count := 0
	unreg := RegisterHook(func(core.Event) { count++ })
	unreg()

	if err := Append(repoRoot, core.Event{
		Actor: "alice", Action: core.ActionCreate, Kind: core.KindDecision,
		Tree: "alpha", ID: "01HZZZZZZZZZZZZZZZZZZZZZZZ",
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if count != 0 {
		t.Errorf("hook fired %d times after unregister, want 0", count)
	}
}

func initTestRepo(root, tree string) error {
	return os.MkdirAll(filepath.Join(root, ".decisions", tree, "audit"), 0o755)
}
