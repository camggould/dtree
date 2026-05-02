// Package ulid wraps oklog/ulid/v2 with a small ergonomic surface for
// dtree: monotonic-by-default IDs in production and a deterministic
// generator for tests. ULIDs are 26-character Crockford base32 — they
// sort lexicographically by creation time and are globally unique
// without coordination, which is the entire reason dtree never sees
// merge conflicts on new IDs.
package ulid

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	uliddep "github.com/oklog/ulid/v2"
)

// Length is the canonical encoded length (26 Crockford base32 chars).
const Length = 26

// Generator produces ULIDs. The default implementation uses the system
// clock and crypto/rand with monotonic guarantees within the same ms.
// Tests can substitute a deterministic generator via NewDeterministic.
type Generator interface {
	// New returns a new ULID. Errors only on entropy-source failure,
	// which is effectively unreachable for crypto/rand.
	New() (string, error)
}

// Default is the package-level generator used by the convenience New().
// Replace at process start (e.g. in tests) before any ULIDs are minted.
var Default Generator = newMonotonic(rand.Reader)

// New returns a new ULID using the package-level Default generator.
// Panics on entropy failure since callers can't meaningfully recover.
func New() string {
	id, err := Default.New()
	if err != nil {
		panic(fmt.Errorf("ulid: %w", err))
	}
	return id
}

// Parse validates the canonical encoding without decoding into a struct.
// Returns nil iff s is a syntactically valid ULID. Does not check that
// the timestamp portion is in any particular range.
func Parse(s string) error {
	if len(s) != Length {
		return fmt.Errorf("ulid: expected %d chars, got %d", Length, len(s))
	}
	if _, err := uliddep.ParseStrict(s); err != nil {
		return fmt.Errorf("ulid: %w", err)
	}
	return nil
}

// Time extracts the embedded timestamp from a ULID. Useful for
// chronological sorting fallbacks when the index isn't available.
func Time(s string) (time.Time, error) {
	parsed, err := uliddep.ParseStrict(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("ulid: %w", err)
	}
	return uliddep.Time(parsed.Time()), nil
}

// monotonic wraps a single MonotonicEntropy and serializes generation
// across goroutines. The standard library's MonotonicEntropy isn't
// safe for concurrent use, so we lock around it.
type monotonic struct {
	mu      sync.Mutex
	entropy *uliddep.MonotonicEntropy
}

func newMonotonic(r io.Reader) *monotonic {
	return &monotonic{entropy: uliddep.Monotonic(r, 0)}
}

func (m *monotonic) New() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, err := uliddep.New(uliddep.Timestamp(time.Now()), m.entropy)
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// NewDeterministic returns a Generator that produces a fixed sequence
// of ULIDs derived from seed. Two generators with the same seed produce
// identical sequences — useful for tests that compare ID-bearing output.
//
// The clock advances by 1ms per call so ULIDs remain monotonic but
// fully reproducible.
func NewDeterministic(seed int64) Generator {
	return &deterministic{
		clock:   time.Unix(seed, 0).UTC(),
		entropy: deterministicEntropy(uint64(seed)),
	}
}

type deterministic struct {
	mu      sync.Mutex
	clock   time.Time
	entropy *deterministicReader
}

func (d *deterministic) New() (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	id, err := uliddep.New(uliddep.Timestamp(d.clock), d.entropy)
	if err != nil {
		return "", err
	}
	d.clock = d.clock.Add(time.Millisecond)
	return id.String(), nil
}

// deterministicReader is a tiny PRNG (xorshift64) seeded from a known
// value. We don't need cryptographic strength here — only reproducibility.
type deterministicReader struct {
	state uint64
}

func deterministicEntropy(seed uint64) *deterministicReader {
	if seed == 0 {
		seed = 0x9E3779B97F4A7C15 // arbitrary nonzero default
	}
	return &deterministicReader{state: seed}
}

func (d *deterministicReader) Read(p []byte) (int, error) {
	if d.state == 0 {
		return 0, errors.New("ulid: deterministic state exhausted")
	}
	for i := range p {
		d.state ^= d.state << 13
		d.state ^= d.state >> 7
		d.state ^= d.state << 17
		p[i] = byte(d.state)
	}
	return len(p), nil
}
