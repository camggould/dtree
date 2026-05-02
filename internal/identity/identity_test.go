package identity_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cgould/dtree/internal/config"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/identity"
	"github.com/cgould/dtree/internal/storage"
)

// helpers

func tempRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".decisions"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeActors(t *testing.T, repoRoot string, actors []core.Actor) {
	t.Helper()
	af := &storage.ActorsFile{Actors: actors}
	path := filepath.Join(repoRoot, ".decisions", storage.ActorsFileName)
	if err := storage.WriteActors(path, af); err != nil {
		t.Fatalf("writeActors: %v", err)
	}
}

func humanActor(handle string) core.Actor {
	return core.Actor{Handle: handle, Name: handle + " User", Kind: core.ActorHuman, Active: true}
}

func emptyCfg() *config.Resolved {
	return &config.Resolved{IdentitySrc: config.SourceDefault}
}

func cfgWithIdentity(handle string, src config.Source) *config.Resolved {
	return &config.Resolved{Identity: handle, IdentitySrc: src}
}

// --- TestResolveFromFlag ---

func TestResolveFromFlag(t *testing.T) {
	repo := tempRepo(t)
	writeActors(t, repo, []core.Actor{humanActor("alice")})

	r := identity.NewResolver(repo, cfgWithIdentity("bob", config.SourceGlobal))
	res, err := r.Resolve("alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Handle != "alice" {
		t.Errorf("Handle = %q; want %q", res.Handle, "alice")
	}
	if res.Source != config.SourceFlag {
		t.Errorf("Source = %q; want %q", res.Source, config.SourceFlag)
	}
	if !res.InProject {
		t.Error("InProject should be true for a registered handle")
	}
	if res.Actor == nil || res.Actor.Handle != "alice" {
		t.Error("Actor should be populated")
	}
}

// --- TestResolveFromConfigGlobal ---

func TestResolveFromConfigGlobal(t *testing.T) {
	repo := tempRepo(t)
	writeActors(t, repo, []core.Actor{humanActor("bob")})

	r := identity.NewResolver(repo, cfgWithIdentity("bob", config.SourceGlobal))
	res, err := r.Resolve("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Handle != "bob" {
		t.Errorf("Handle = %q; want %q", res.Handle, "bob")
	}
	if res.Source != config.SourceGlobal {
		t.Errorf("Source = %q; want %q", res.Source, config.SourceGlobal)
	}
}

// --- TestResolveFromConfigLocal ---

func TestResolveFromConfigLocal(t *testing.T) {
	repo := tempRepo(t)
	writeActors(t, repo, []core.Actor{humanActor("carol")})

	// Local source overrides global
	r := identity.NewResolver(repo, cfgWithIdentity("carol", config.SourceLocal))
	res, err := r.Resolve("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Source != config.SourceLocal {
		t.Errorf("Source = %q; want %q", res.Source, config.SourceLocal)
	}
	if res.Handle != "carol" {
		t.Errorf("Handle = %q; want %q", res.Handle, "carol")
	}
}

// --- TestResolveEmpty ---

func TestResolveEmpty(t *testing.T) {
	repo := tempRepo(t)
	r := identity.NewResolver(repo, emptyCfg())
	res, err := r.Resolve("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Handle != "" {
		t.Errorf("Handle = %q; want empty", res.Handle)
	}
	if res.Source != config.SourceDefault {
		t.Errorf("Source = %q; want %q", res.Source, config.SourceDefault)
	}
	if res.InProject {
		t.Error("InProject should be false when handle is empty")
	}
}

// --- TestResolveValidatesAgainstProjectActors ---

func TestResolveValidatesAgainstProjectActors(t *testing.T) {
	repo := tempRepo(t)
	// actors.yaml does not include "dave"
	writeActors(t, repo, []core.Actor{humanActor("alice")})

	r := identity.NewResolver(repo, cfgWithIdentity("dave", config.SourceGlobal))
	res, err := r.Resolve("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Handle != "dave" {
		t.Errorf("Handle = %q; want %q", res.Handle, "dave")
	}
	if res.InProject {
		t.Error("InProject should be false for a handle not in actors.yaml")
	}
	if res.Actor != nil {
		t.Error("Actor should be nil when not in project")
	}
}

// --- TestResolveInProject ---

func TestResolveInProject(t *testing.T) {
	repo := tempRepo(t)
	writeActors(t, repo, []core.Actor{humanActor("eve")})

	r := identity.NewResolver(repo, cfgWithIdentity("eve", config.SourceGlobal))
	res, err := r.Resolve("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.InProject {
		t.Error("InProject should be true")
	}
	if res.Actor == nil || res.Actor.Handle != "eve" {
		t.Error("Actor should be populated with the matching record")
	}
}

// --- TestMustResolveErrors ---

func TestMustResolveErrors(t *testing.T) {
	repo := tempRepo(t)
	r := identity.NewResolver(repo, emptyCfg())
	_, err := r.MustResolve("")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if want := "no identity configured"; !contains(err.Error(), want) {
		t.Errorf("error %q should mention %q", err.Error(), want)
	}
	if want := "dtree config set"; !contains(err.Error(), want) {
		t.Errorf("error %q should mention %q", err.Error(), want)
	}
}

// --- TestMustResolveSuggestsAdd ---

func TestMustResolveSuggestsAdd(t *testing.T) {
	repo := tempRepo(t)
	// actors.yaml exists but does not contain "frank"
	writeActors(t, repo, []core.Actor{humanActor("alice")})

	r := identity.NewResolver(repo, cfgWithIdentity("frank", config.SourceGlobal))
	_, err := r.MustResolve("")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if want := "frank"; !contains(err.Error(), want) {
		t.Errorf("error %q should mention the handle %q", err.Error(), want)
	}
	if want := "dtree actor add"; !contains(err.Error(), want) {
		t.Errorf("error %q should mention %q", err.Error(), want)
	}
}

// --- TestLoadActorsMissingFile ---

func TestLoadActorsMissingFile(t *testing.T) {
	repo := tempRepo(t)
	// actors.yaml does not exist
	r := identity.NewResolver(repo, emptyCfg())
	af, err := r.LoadActors()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if af == nil {
		t.Fatal("returned nil ActorsFile; want empty")
	}
	if len(af.Actors) != 0 {
		t.Errorf("expected 0 actors, got %d", len(af.Actors))
	}
}

// --- TestAddActor ---

func TestAddActor(t *testing.T) {
	repo := tempRepo(t)
	r := identity.NewResolver(repo, emptyCfg())

	a := humanActor("grace")
	if err := r.AddActor(a); err != nil {
		t.Fatalf("AddActor: %v", err)
	}

	found, err := r.FindActor("grace")
	if err != nil {
		t.Fatalf("FindActor: %v", err)
	}
	if found == nil {
		t.Fatal("FindActor returned nil; expected actor")
	}
	if found.Handle != "grace" {
		t.Errorf("Handle = %q; want %q", found.Handle, "grace")
	}
}

// --- TestAddActorIdempotent ---

func TestAddActorIdempotent(t *testing.T) {
	repo := tempRepo(t)
	r := identity.NewResolver(repo, emptyCfg())

	a := humanActor("heidi")
	if err := r.AddActor(a); err != nil {
		t.Fatalf("first AddActor: %v", err)
	}
	if err := r.AddActor(a); err != nil {
		t.Fatalf("second AddActor (idempotent): %v", err)
	}

	// Ensure only one record.
	af, _ := r.LoadActors()
	count := 0
	for _, actor := range af.Actors {
		if actor.Handle == "heidi" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 record for %q, got %d", "heidi", count)
	}
}

// --- TestAddActorConflictDifferentFields ---

func TestAddActorConflictDifferentFields(t *testing.T) {
	repo := tempRepo(t)
	r := identity.NewResolver(repo, emptyCfg())

	a := humanActor("ivan")
	if err := r.AddActor(a); err != nil {
		t.Fatalf("AddActor: %v", err)
	}

	// Same handle, different name.
	a2 := a
	a2.Name = "Different Name"
	err := r.AddActor(a2)
	if !errors.Is(err, identity.ErrActorExists) {
		t.Errorf("expected ErrActorExists, got %v", err)
	}
}

// --- TestAddActorInvalidHandle ---

func TestAddActorInvalidHandle(t *testing.T) {
	repo := tempRepo(t)
	r := identity.NewResolver(repo, emptyCfg())

	cases := []struct {
		name   string
		handle string
	}{
		{"empty", ""},
		{"whitespace", "has space"},
		{"tab", "has\ttab"},
		{"too long", "a" + string(make([]byte, 64))}, // 65+ chars
		{"starts with digit", "1invalid"},
		{"starts with dot", ".invalid"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := core.Actor{Handle: tc.handle, Kind: core.ActorHuman, Active: true}
			err := r.AddActor(a)
			if !errors.Is(err, identity.ErrInvalidHandle) {
				t.Errorf("handle %q: expected ErrInvalidHandle, got %v", tc.handle, err)
			}
		})
	}
}

// --- TestUpdateActor ---

func TestUpdateActor(t *testing.T) {
	repo := tempRepo(t)
	r := identity.NewResolver(repo, emptyCfg())

	a := humanActor("judy")
	if err := r.AddActor(a); err != nil {
		t.Fatal(err)
	}

	if err := r.UpdateActor("judy", func(a *core.Actor) {
		a.Name = "Judith Updated"
	}); err != nil {
		t.Fatalf("UpdateActor: %v", err)
	}

	found, _ := r.FindActor("judy")
	if found == nil {
		t.Fatal("FindActor returned nil after update")
	}
	if found.Name != "Judith Updated" {
		t.Errorf("Name = %q; want %q", found.Name, "Judith Updated")
	}
	if found.Handle != "judy" {
		t.Errorf("Handle changed after update; got %q", found.Handle)
	}
}

// --- TestUpdateActorNotFound ---

func TestUpdateActorNotFound(t *testing.T) {
	repo := tempRepo(t)
	r := identity.NewResolver(repo, emptyCfg())

	err := r.UpdateActor("nobody", func(a *core.Actor) { a.Name = "X" })
	if !errors.Is(err, identity.ErrActorNotFound) {
		t.Errorf("expected ErrActorNotFound, got %v", err)
	}
}

// --- TestRenameActor ---

func TestRenameActor(t *testing.T) {
	repo := tempRepo(t)
	r := identity.NewResolver(repo, emptyCfg())

	a := humanActor("karl")
	if err := r.AddActor(a); err != nil {
		t.Fatal(err)
	}

	if err := r.RenameActor("karl", "karl2"); err != nil {
		t.Fatalf("RenameActor: %v", err)
	}

	old, _ := r.FindActor("karl")
	if old != nil {
		t.Error("old handle should not be found after rename")
	}

	newA, _ := r.FindActor("karl2")
	if newA == nil {
		t.Fatal("new handle not found after rename")
	}
	if newA.Name != a.Name {
		t.Errorf("Name = %q after rename; want %q", newA.Name, a.Name)
	}
}

// --- TestRenameActorNotFound ---

func TestRenameActorNotFound(t *testing.T) {
	repo := tempRepo(t)
	r := identity.NewResolver(repo, emptyCfg())

	err := r.RenameActor("nobody", "somebody")
	if !errors.Is(err, identity.ErrActorNotFound) {
		t.Errorf("expected ErrActorNotFound, got %v", err)
	}
}

// --- TestRenameActorConflict ---

func TestRenameActorConflict(t *testing.T) {
	repo := tempRepo(t)
	r := identity.NewResolver(repo, emptyCfg())

	if err := r.AddActor(humanActor("leo")); err != nil {
		t.Fatal(err)
	}
	if err := r.AddActor(humanActor("mia")); err != nil {
		t.Fatal(err)
	}

	err := r.RenameActor("leo", "mia")
	if !errors.Is(err, identity.ErrActorExists) {
		t.Errorf("expected ErrActorExists, got %v", err)
	}
}

// --- TestArchiveActor ---

func TestArchiveActor(t *testing.T) {
	repo := tempRepo(t)
	r := identity.NewResolver(repo, emptyCfg())

	a := humanActor("ned")
	if err := r.AddActor(a); err != nil {
		t.Fatal(err)
	}

	if err := r.ArchiveActor("ned"); err != nil {
		t.Fatalf("ArchiveActor: %v", err)
	}

	found, _ := r.FindActor("ned")
	if found == nil {
		t.Fatal("actor should still exist after archive")
	}
	if found.Active {
		t.Error("Active should be false after archive")
	}
}

// --- TestArchiveActorAlreadyArchived ---

func TestArchiveActorAlreadyArchived(t *testing.T) {
	repo := tempRepo(t)
	r := identity.NewResolver(repo, emptyCfg())

	a := core.Actor{Handle: "olivia", Kind: core.ActorHuman, Active: false}
	if err := r.AddActor(a); err != nil {
		t.Fatal(err)
	}

	err := r.ArchiveActor("olivia")
	if !errors.Is(err, identity.ErrActorAlreadyArchived) {
		t.Errorf("expected ErrActorAlreadyArchived, got %v", err)
	}
}

// --- TestActorsFileNotMutatedOnFailedWrite ---
// Verifies that a validation failure before write leaves the on-disk file
// unchanged. We exercise this by attempting to AddActor with an invalid
// handle (fails before touching disk) and then confirming the file state.

func TestActorsFileNotMutatedOnFailedWrite(t *testing.T) {
	repo := tempRepo(t)
	r := identity.NewResolver(repo, emptyCfg())

	// Establish a known-good state.
	if err := r.AddActor(humanActor("pat")); err != nil {
		t.Fatal(err)
	}

	// Read the file content before the failed attempt.
	path := filepath.Join(repo, ".decisions", storage.ActorsFileName)
	beforeBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Attempt an invalid add; should fail without touching disk.
	_ = r.AddActor(core.Actor{Handle: "", Kind: core.ActorHuman, Active: true})

	afterBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if string(beforeBytes) != string(afterBytes) {
		t.Error("actors.yaml was modified despite a failed AddActor call")
	}
}

// contains is a helper for simple substring checks.
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
