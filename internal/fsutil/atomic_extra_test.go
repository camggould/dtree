package fsutil

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// WriteAtomic — write to a non-existent directory (should auto-create)
// ---------------------------------------------------------------------------

func TestWriteAtomicCreatesIntermediateDirs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Path with multiple non-existent intermediate directories.
	path := filepath.Join(dir, "a", "b", "c", "file.yaml")
	if err := WriteAtomic(path, []byte("data"), 0o644); err != nil {
		t.Fatalf("WriteAtomic with non-existent dirs: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "data" {
		t.Errorf("got %q, want 'data'", got)
	}
}

// ---------------------------------------------------------------------------
// WriteAtomic — empty data
// ---------------------------------------------------------------------------

func TestWriteAtomicEmptyData(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.yaml")
	if err := WriteAtomic(path, []byte{}, 0o644); err != nil {
		t.Fatalf("WriteAtomic empty: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("expected empty file, got size %d", info.Size())
	}
}

// ---------------------------------------------------------------------------
// WriteAtomic — concurrent writes to different paths (race detector safe)
// ---------------------------------------------------------------------------

func TestWriteAtomicConcurrentDifferentPaths(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			path := filepath.Join(dir, "file-%d.yaml")
			_ = WriteAtomic(filepath.Clean(filepath.Join(dir, filepath.Base(path[:len(path)-1])+string(rune('0'+i)))), []byte("content"), 0o644)
		}()
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// WriteAtomic — concurrent writes to the same path
// ---------------------------------------------------------------------------

func TestWriteAtomicConcurrentSamePath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "shared.yaml")

	const n = 30
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_ = WriteAtomic(path, []byte("concurrent-write"), 0o644)
		}()
	}
	wg.Wait()

	// File must exist and have valid contents (last writer wins).
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "concurrent-write" {
		t.Errorf("unexpected content: %q", got)
	}
}

// ---------------------------------------------------------------------------
// AppendLine — data already ends with newline
// ---------------------------------------------------------------------------

func TestAppendLineDataAlreadyHasNewline(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	if err := AppendLine(path, []byte("line1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	// Should not double-newline.
	if string(got) != "line1\n" {
		t.Errorf("got %q, want exactly 'line1\\n'", got)
	}
}

// ---------------------------------------------------------------------------
// AppendLine — creates intermediate dirs
// ---------------------------------------------------------------------------

func TestAppendLineCreatesDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "dir", "audit.jsonl")
	if err := AppendLine(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("AppendLine to nested path: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "x\n" {
		t.Errorf("got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Sha256File — non-existent file returns error
// ---------------------------------------------------------------------------

func TestSha256FileNonExistent(t *testing.T) {
	t.Parallel()
	_, err := Sha256File("/nonexistent/path/file.txt")
	if err == nil {
		t.Error("expected error for non-existent file, got nil")
	}
}

// ---------------------------------------------------------------------------
// AcquireLockBlocking — basic operation
// ---------------------------------------------------------------------------

func TestAcquireLockBlocking(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "blocking.lock")

	lock, err := AcquireLockBlocking(path)
	if err != nil {
		t.Fatalf("AcquireLockBlocking: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	// Can re-acquire after release.
	lock2, err := AcquireLockBlocking(path)
	if err != nil {
		t.Fatalf("second AcquireLockBlocking: %v", err)
	}
	_ = lock2.Release()
}

// ---------------------------------------------------------------------------
// FileLock.Release — safe to call on nil lock
// ---------------------------------------------------------------------------

func TestFileLockReleaseNil(t *testing.T) {
	t.Parallel()
	var l *FileLock
	if err := l.Release(); err != nil {
		t.Errorf("Release on nil lock: %v", err)
	}
}

// ---------------------------------------------------------------------------
// FileLock.Release — idempotent (double release doesn't panic)
// ---------------------------------------------------------------------------

func TestFileLockDoubleRelease(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, ".lock2")

	lock, err := AcquireLock(path)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("first Release: %v", err)
	}
	// Second Release should not panic; the fd is nil.
	if err := lock.Release(); err != nil {
		t.Errorf("second Release (double): %v", err)
	}
}
