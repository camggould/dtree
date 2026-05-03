package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenameWithExplicitSlug(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := makeProposed(t, repoRoot, "Original summary")

	if _, _, err := runCmdWithStdin(t, "", "--repo-root", repoRoot,
		"rename", id, "new-slug",
	); err != nil {
		t.Fatalf("rename: %v", err)
	}

	dir := filepath.Join(repoRoot, ".decisions", "backend", "decisions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	var found string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), id) {
			found = e.Name()
			break
		}
	}
	if found == "" {
		t.Fatalf("renamed file not found in %s", dir)
	}
	if !strings.Contains(found, "new-slug") {
		t.Errorf("expected new-slug in filename, got %q", found)
	}
}

func TestRenameNoOpWhenSlugMatches(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := makeProposed(t, repoRoot, "Same summary")

	dir := filepath.Join(repoRoot, ".decisions", "backend", "decisions")
	entries, _ := os.ReadDir(dir)
	var current string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), id) {
			current = e.Name()
		}
	}
	parts := strings.SplitN(strings.TrimSuffix(current, ".yaml"), "-", 2)
	if len(parts) != 2 {
		t.Fatalf("unexpected filename format: %q", current)
	}
	currentSlug := parts[1]

	stdout, _, err := runCmdWithStdin(t, "", "--repo-root", repoRoot,
		"rename", id, currentSlug,
	)
	if err != nil {
		t.Fatalf("rename no-op: %v (stdout=%q)", err, stdout)
	}
}
