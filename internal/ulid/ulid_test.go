package ulid

import (
	"strings"
	"testing"
)

func TestNewProducesValidULID(t *testing.T) {
	id := New()
	if err := Parse(id); err != nil {
		t.Fatalf("Parse(New()) failed: %v", err)
	}
	if got := len(id); got != Length {
		t.Errorf("length = %d, want %d", got, Length)
	}
}

func TestParseRejectsWrongLength(t *testing.T) {
	if err := Parse("short"); err == nil {
		t.Error("expected error for short ULID")
	}
}

func TestParseRejectsInvalidChars(t *testing.T) {
	// 'I' is excluded from Crockford base32 (looks like 1).
	bad := strings.Repeat("I", Length)
	if err := Parse(bad); err == nil {
		t.Error("expected error for ULID with invalid characters")
	}
}

func TestNewMonotonic(t *testing.T) {
	const n = 1000
	prev := New()
	for i := 0; i < n; i++ {
		id := New()
		if id <= prev {
			t.Fatalf("ULIDs not monotonic at iter %d: %q !> %q", i, id, prev)
		}
		prev = id
	}
}

func TestDeterministicReproducible(t *testing.T) {
	a := NewDeterministic(42)
	b := NewDeterministic(42)
	for i := 0; i < 5; i++ {
		ax, err := a.New()
		if err != nil {
			t.Fatal(err)
		}
		bx, err := b.New()
		if err != nil {
			t.Fatal(err)
		}
		if ax != bx {
			t.Fatalf("iter %d: %q != %q (deterministic should be reproducible)", i, ax, bx)
		}
	}
}

func TestDeterministicMonotonic(t *testing.T) {
	g := NewDeterministic(1)
	prev, _ := g.New()
	for i := 0; i < 100; i++ {
		id, err := g.New()
		if err != nil {
			t.Fatal(err)
		}
		if id <= prev {
			t.Fatalf("deterministic ULIDs not monotonic at iter %d: %q !> %q", i, id, prev)
		}
		prev = id
	}
}

func TestTimeRoundTrip(t *testing.T) {
	g := NewDeterministic(1700000000)
	id, _ := g.New()
	got, err := Time(id)
	if err != nil {
		t.Fatal(err)
	}
	// Deterministic generator starts at seed-as-unix-seconds.
	if want := int64(1700000000); got.Unix() != want {
		t.Errorf("Time = %d, want %d", got.Unix(), want)
	}
}
