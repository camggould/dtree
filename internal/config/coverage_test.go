package config

import (
	"testing"
)

func TestResolvedGet(t *testing.T) {
	r := &Resolved{
		Identity: "alice", IdentitySrc: SourceGlobal,
		Editor: "vim", EditorSrc: SourceLocal,
		Output: "json", OutputSrc: SourceEnv,
		Color: "auto", ColorSrc: SourceDefault,
		DefaultTree: "alpha", DefaultTreeSrc: SourceLocal,
	}

	cases := []struct {
		key     string
		want    string
		wantSrc Source
		wantOK  bool
	}{
		{"identity.default", "alice", SourceGlobal, true},
		{"editor", "vim", SourceLocal, true},
		{"output", "json", SourceEnv, true},
		{"color", "auto", SourceDefault, true},
		{"default_tree", "alpha", SourceLocal, true},
		{"bogus", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			got, gotSrc, ok := r.Get(c.key)
			if got != c.want || gotSrc != c.wantSrc || ok != c.wantOK {
				t.Errorf("Get(%q) = (%q, %q, %v); want (%q, %q, %v)",
					c.key, got, gotSrc, ok, c.want, c.wantSrc, c.wantOK)
			}
		})
	}
}

func TestSetFieldUnknownKey(t *testing.T) {
	f := &File{}
	if err := setField(f, "unknown.key", "v"); err == nil {
		t.Error("expected error for unknown key")
	}
}

func TestSetFieldKnownKeys(t *testing.T) {
	f := &File{}
	if err := setField(f, "identity.default", "alice"); err != nil {
		t.Fatal(err)
	}
	if f.Identity.Default != "alice" {
		t.Errorf("identity: got %q", f.Identity.Default)
	}
	if err := setField(f, "editor", "nvim"); err != nil {
		t.Fatal(err)
	}
	if f.Editor != "nvim" {
		t.Errorf("editor: got %q", f.Editor)
	}
	if err := setField(f, "output", "yaml"); err != nil {
		t.Fatal(err)
	}
	if err := setField(f, "color", "always"); err != nil {
		t.Fatal(err)
	}
	if err := setField(f, "default_tree", "beta"); err != nil {
		t.Fatal(err)
	}
	if f.DefaultTree != "beta" {
		t.Errorf("default_tree: got %q", f.DefaultTree)
	}
}

func TestGlobalPathRespectsXDGAndHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/xdg")
	got, err := GlobalPath()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/custom/xdg/dtree/config.yaml" {
		t.Errorf("XDG override: got %q", got)
	}

	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/custom/home")
	got, err = GlobalPath()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/custom/home/.config/dtree/config.yaml" {
		t.Errorf("HOME fallback: got %q", got)
	}
}
