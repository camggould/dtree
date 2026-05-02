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

	v, err := db.SchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v != 0 {
		t.Errorf("fresh DB schema_version = %d, want 0", v)
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
	if err := db.SetSchemaVersion(3); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	db2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	got, _ := db2.SchemaVersion()
	if got != 3 {
		t.Errorf("got %d, want 3", got)
	}
}
