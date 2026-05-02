// Package fsutil provides safe filesystem primitives for dtree:
// atomic writes (tmp + fsync + rename), append-only line writes,
// content hashing, and exclusive file locking. These are the building
// blocks every storage path uses; doing them correctly here means
// no caller has to reason about partial writes or torn reads.
package fsutil

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// WriteAtomic writes data to path durably: it stages a temp file in the
// same directory, fsyncs the file, then renames over the destination.
// On POSIX, rename is atomic on the same filesystem, so readers either
// see the old or new contents — never a partial.
//
// mode is the final file's permission bits. The temp file has the same
// permissions, so a crash between rename and a permission set wouldn't
// leave a too-permissive file on disk.
func WriteAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("fsutil: mkdir %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".dtree-tmp-*")
	if err != nil {
		return fmt.Errorf("fsutil: create temp: %w", err)
	}
	tmpPath := tmp.Name()
	// On any failure beyond this point, leave no temp behind.
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("fsutil: write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("fsutil: fsync temp: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("fsutil: chmod temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("fsutil: close temp: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("fsutil: rename %s → %s: %w", tmpPath, path, err)
	}

	// Best-effort dir fsync so the rename itself is durable. Some
	// filesystems don't require it, but it's cheap insurance.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// AppendLine appends a line of data (with a trailing newline) to path,
// creating the file if missing. Uses O_APPEND, which the kernel makes
// atomic for writes smaller than PIPE_BUF (~4KB on Linux). Audit events
// are well under that ceiling, so concurrent writers don't interleave.
func AppendLine(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("fsutil: mkdir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("fsutil: open %s: %w", path, err)
	}
	defer f.Close()

	buf := make([]byte, 0, len(data)+1)
	buf = append(buf, data...)
	if len(data) == 0 || data[len(data)-1] != '\n' {
		buf = append(buf, '\n')
	}
	if _, err := f.Write(buf); err != nil {
		return fmt.Errorf("fsutil: append: %w", err)
	}
	return nil
}

// Sha256File returns the hex-encoded SHA-256 of path's contents. Used
// to detect external edits: the index stores this hash, and on every
// CLI invocation we compare against the file on disk.
func Sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("fsutil: open %s: %w", path, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("fsutil: read %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// FileLock is an exclusive advisory lock held on a file descriptor.
// Used to serialize index rebuilds across processes.
type FileLock struct {
	f *os.File
}

// AcquireLock opens path (creating if missing) and takes an exclusive
// flock. Returns an error immediately if the lock is already held by
// another process; use AcquireLockBlocking if you want to wait.
//
// The returned lock must be closed via Release; the underlying file
// descriptor stays open until then.
func AcquireLock(path string) (*FileLock, error) {
	return acquireLock(path, syscall.LOCK_EX|syscall.LOCK_NB)
}

// AcquireLockBlocking takes an exclusive flock, waiting if necessary.
func AcquireLockBlocking(path string) (*FileLock, error) {
	return acquireLock(path, syscall.LOCK_EX)
}

func acquireLock(path string, how int) (*FileLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("fsutil: mkdir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("fsutil: open lockfile %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), how); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("fsutil: lock held by another process: %s", path)
		}
		return nil, fmt.Errorf("fsutil: flock %s: %w", path, err)
	}
	return &FileLock{f: f}, nil
}

// Release drops the flock and closes the descriptor. Safe to call once.
func (l *FileLock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	// Linux: closing the fd releases the flock. Explicit unlock keeps
	// behavior portable across platforms that may differ.
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	err := l.f.Close()
	l.f = nil
	return err
}
