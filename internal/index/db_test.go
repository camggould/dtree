package index

import (
	"path/filepath"
	"testing"
)

func TestOpenCreatesDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".index.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Open now calls CreateSchema, which stamps schema_version.
	v, err := db.SchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v != CurrentSchemaVersion {
		t.Errorf("fresh DB schema_version = %d, want %d", v, CurrentSchemaVersion)
	}
}

func TestSchemaVersionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, ".index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.SetSchemaVersion(7); err != nil {
		t.Fatal(err)
	}
	got, err := db.SchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if got != 7 {
		t.Errorf("got %d, want 7", got)
	}

	// Idempotent upsert.
	if err := db.SetSchemaVersion(8); err != nil {
		t.Fatal(err)
	}
	got, _ = db.SchemaVersion()
	if got != 8 {
		t.Errorf("after update got %d, want 8", got)
	}
}

func TestOpenSetsWALMode(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, ".index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var mode string
	if err := db.Conn().QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
}

func TestForeignKeysEnabled(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, ".index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var fk int
	if err := db.Conn().QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d, want 1", fk)
	}
}

func TestReopenPreservesSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".index.db")

	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// Use CurrentSchemaVersion so Open() accepts the version on reopen.
	if err := db.SetSchemaVersion(CurrentSchemaVersion); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	db2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	got, _ := db2.SchemaVersion()
	if got != CurrentSchemaVersion {
		t.Errorf("got %d, want %d", got, CurrentSchemaVersion)
	}
}

// TestOpenRejectsTooHighSchemaVersion verifies that Open returns an error
// when the on-disk schema_version exceeds what this binary supports.
func TestOpenRejectsTooHighSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".index.db")

	// Open once to create the schema, then manually bump version beyond
	// CurrentSchemaVersion to simulate a DB from a future binary.
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetSchemaVersion(CurrentSchemaVersion + 1); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	// Reopen should fail.
	_, err = Open(path)
	if err == nil {
		t.Fatal("Open with future schema_version: expected error, got nil")
	}
}
