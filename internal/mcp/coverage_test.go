package mcp

import (
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// ---- arg helpers ----------------------------------------------------------

func makeReqWithArgs(args map[string]any) mcpgo.CallToolRequest {
	return mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Arguments: args,
		},
	}
}

func TestArgString(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		key  string
		want string
	}{
		{"present", map[string]any{"k": "v"}, "k", "v"},
		{"missing", map[string]any{"k": "v"}, "x", ""},
		{"wrong type", map[string]any{"k": 42}, "k", ""},
		{"nil args", nil, "k", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := argString(makeReqWithArgs(c.args), c.key)
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestArgStringPtr(t *testing.T) {
	if argStringPtr(makeReqWithArgs(nil), "k") != nil {
		t.Error("nil args should return nil")
	}
	if argStringPtr(makeReqWithArgs(map[string]any{}), "k") != nil {
		t.Error("missing key should return nil")
	}
	got := argStringPtr(makeReqWithArgs(map[string]any{"k": "v"}), "k")
	if got == nil || *got != "v" {
		t.Errorf("got %v, want &\"v\"", got)
	}
	if argStringPtr(makeReqWithArgs(map[string]any{"k": 1}), "k") != nil {
		t.Error("wrong type should return nil")
	}
}

func TestArgBool(t *testing.T) {
	if argBool(makeReqWithArgs(nil), "k") {
		t.Error("nil args should return false")
	}
	if argBool(makeReqWithArgs(map[string]any{"k": "true"}), "k") {
		t.Error("string should return false (wrong type)")
	}
	if !argBool(makeReqWithArgs(map[string]any{"k": true}), "k") {
		t.Error("true bool should return true")
	}
}

func TestArgInt(t *testing.T) {
	cases := []struct {
		name string
		val  any
		want int
	}{
		{"float64", float64(42), 42},
		{"int", int(7), 7},
		{"int64", int64(99), 99},
		{"string", "x", 0},
		{"missing", nil, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args := map[string]any{}
			if c.val != nil {
				args["k"] = c.val
			}
			if got := argInt(makeReqWithArgs(args), "k"); got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
	if argInt(makeReqWithArgs(nil), "k") != 0 {
		t.Error("nil args should return 0")
	}
}

func TestArgStringSlice(t *testing.T) {
	if argStringSlice(makeReqWithArgs(nil), "k") != nil {
		t.Error("nil args should return nil")
	}
	if argStringSlice(makeReqWithArgs(map[string]any{}), "k") != nil {
		t.Error("missing key should return nil")
	}
	got := argStringSlice(makeReqWithArgs(map[string]any{"k": []string{"a", "b"}}), "k")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("got %v, want [a b]", got)
	}
	got = argStringSlice(makeReqWithArgs(map[string]any{"k": []any{"a", 42, "b"}}), "k")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("filtered: got %v, want [a b]", got)
	}
}

// ---- parseSince -----------------------------------------------------------

func TestParseSince(t *testing.T) {
	now := time.Now().UTC()

	t.Run("empty", func(t *testing.T) {
		got, err := parseSince("")
		if err != nil {
			t.Fatal(err)
		}
		if !got.IsZero() {
			t.Errorf("want zero, got %v", got)
		}
	})

	t.Run("rfc3339", func(t *testing.T) {
		want := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
		got, err := parseSince(want.Format(time.RFC3339))
		if err != nil || !got.Equal(want) {
			t.Errorf("got %v err %v, want %v", got, err, want)
		}
	})

	for _, c := range []struct {
		s   string
		min time.Duration
	}{
		{"5d", 5 * 24 * time.Hour},
		{"3h", 3 * time.Hour},
		{"30m", 30 * time.Minute},
		{"15s", 15 * time.Second},
	} {
		t.Run(c.s, func(t *testing.T) {
			got, err := parseSince(c.s)
			if err != nil {
				t.Fatal(err)
			}
			delta := now.Sub(got)
			if delta < c.min-time.Second || delta > c.min+time.Second {
				t.Errorf("%s: delta %v not ~= %v", c.s, delta, c.min)
			}
		})
	}

	for _, bad := range []string{"x", "5x", "qd", "1"} {
		t.Run("bad/"+bad, func(t *testing.T) {
			if _, err := parseSince(bad); err == nil {
				t.Errorf("expected error for %q", bad)
			}
		})
	}
}

// ---- containsNoSuchTable --------------------------------------------------

func TestContainsNoSuchTable(t *testing.T) {
	cases := []struct {
		s    string
		want bool
	}{
		{"no such table: foo", true},
		{"sql error: no such table: bar", true},
		{"some other error", false},
		{"", false},
	}
	for _, c := range cases {
		if got := containsNoSuchTable(c.s); got != c.want {
			t.Errorf("%q: got %v, want %v", c.s, got, c.want)
		}
	}
}

func TestContainsSubstring(t *testing.T) {
	if !containsSubstring("hello world", "world") {
		t.Error("substring not found")
	}
	if containsSubstring("a", "abc") {
		t.Error("substring should not be found in shorter string")
	}
}

