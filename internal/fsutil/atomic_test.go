package fsutil

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestWriteAtomicCreatesAndOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "decision.yaml")

	if err := WriteAtomic(path, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "first" {
		t.Errorf("got %q, want %q", got, "first")
	}

	if err := WriteAtomic(path, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	if string(got) != "second" {
		t.Errorf("got %q, want %q", got, "second")
	}
}

func TestWriteAtomicLeavesNoTempBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.yaml")
	if err := WriteAtomic(path, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".dtree-tmp-") {
			t.Errorf("found leftover temp file: %s", e.Name())
		}
	}
}

func TestAppendLineAddsTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	if err := AppendLine(path, []byte("line1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AppendLine(path, []byte("line2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "line1\nline2\n" {
		t.Errorf("got %q", got)
	}
}

func TestAppendLineConcurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			line := strings.Repeat("x", 100) // well under PIPE_BUF
			_ = AppendLine(path, []byte(line), 0o644)
		}(i)
	}
	wg.Wait()

	data, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != goroutines {
		t.Errorf("expected %d lines, got %d", goroutines, len(lines))
	}
	for i, l := range lines {
		if l != strings.Repeat("x", 100) {
			t.Errorf("line %d torn: %q", i, l)
		}
	}
}

func TestSha256File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	// sha256("hello") = 2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
	got, err := Sha256File(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestFileLockExclusive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".lock")

	first, err := AcquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()

	if _, err := AcquireLock(path); err == nil {
		t.Error("expected second acquire to fail (exclusive lock)")
	}
}

func TestFileLockReleaseAllowsReacquire(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".lock")

	first, err := AcquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}

	second, err := AcquireLock(path)
	if err != nil {
		t.Fatalf("expected reacquire to succeed: %v", err)
	}
	_ = second.Release()
}
