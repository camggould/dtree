// Package config implements three-layer configuration resolution for dtree:
// global (~/.config/dtree/config.yaml), local (<repoRoot>/.decisions/config.yaml),
// and environment variables (DTREE_AS, DTREE_TREE, etc.).
//
// Resolution order (highest to lowest priority):
//   flags > env > local > global > defaults
//
// The package only handles the env/local/global layers; the CLI layer
// applies flag overrides after calling Load.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/fsutil"
	"gopkg.in/yaml.v3"
)

// Source identifies which configuration layer supplied a value.
type Source string

const (
	SourceFlag    Source = "flag"
	SourceEnv     Source = "env"
	SourceLocal   Source = "local"
	SourceGlobal  Source = "global"
	SourceDefault Source = "default"
)

// FileMode is the permission bits for config files.
const FileMode os.FileMode = 0o644

// File is the on-disk YAML shape for a dtree config file.
type File struct {
	SchemaVersion int            `yaml:"schema_version"`
	Identity      IdentityConfig `yaml:"identity,omitempty"`
	Editor        string         `yaml:"editor,omitempty"`
	Output        string         `yaml:"output,omitempty"`
	Color         string         `yaml:"color,omitempty"`
	DefaultTree   string         `yaml:"default_tree,omitempty"`
}

// IdentityConfig holds the active handle and the personal catalog of identities.
type IdentityConfig struct {
	Default    string       `yaml:"default,omitempty"`
	Identities []core.Actor `yaml:"identities,omitempty"`
}

// Resolved holds the merged effective configuration along with source-layer
// tracking for each key.
type Resolved struct {
	Identity       string
	IdentitySrc    Source
	Editor         string
	EditorSrc      Source
	Output         string
	OutputSrc      Source
	Color          string
	ColorSrc       Source
	DefaultTree    string
	DefaultTreeSrc Source

	Global *File // raw loaded global config, may be nil
	Local  *File // raw loaded local config, may be nil
}

// GlobalPath returns the path to the global config file.
//
// Resolution order:
//  1. $XDG_CONFIG_HOME/dtree/config.yaml — honored on every OS (the Go
//     stdlib only consults XDG_CONFIG_HOME on linux/bsd; we honor it
//     explicitly so test setups and macOS users who prefer the XDG
//     layout get a single, predictable answer).
//  2. os.UserConfigDir()/dtree/config.yaml — OS-default location
//     (~/Library/Application Support on darwin, %AppData% on windows).
//  3. $HOME/.dtree/config.yaml — last-resort fallback if no config dir
//     can be determined.
func GlobalPath() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "dtree", "config.yaml"), nil
	}
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "dtree", "config.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".dtree", "config.yaml"), nil
}

// LocalPath returns the path to the local config file within a repo root.
func LocalPath(repoRoot string) string {
	return filepath.Join(repoRoot, ".decisions", "config.yaml")
}

// ReadFile reads and unmarshals the config file at path. If the file does
// not exist, it returns (nil, nil) — a missing config is not an error.
// Sets SchemaVersion to core.SchemaVersion if the parsed value is zero.
func ReadFile(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	var f File
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	if f.SchemaVersion == 0 {
		f.SchemaVersion = core.SchemaVersion
	}
	return &f, nil
}

// WriteFile atomically writes the config file to path, creating the parent
// directory if needed. Sets SchemaVersion to core.SchemaVersion if zero.
func WriteFile(path string, f *File) error {
	if f.SchemaVersion == 0 {
		f.SchemaVersion = core.SchemaVersion
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("config: mkdir %s: %w", filepath.Dir(path), err)
	}
	data, err := marshal(f)
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	if err := fsutil.WriteAtomic(path, data, FileMode); err != nil {
		return fmt.Errorf("config: write %s: %w", path, err)
	}
	return nil
}

// Load loads the global and local config files, merges them with environment
// variables, and returns a Resolved config. If repoRoot is empty, the local
// layer is skipped.
//
// Resolution order (highest to lowest): env > local > global > defaults.
func Load(repoRoot string) (*Resolved, error) {
	globalPath, err := GlobalPath()
	if err != nil {
		return nil, err
	}

	global, err := ReadFile(globalPath)
	if err != nil {
		return nil, err
	}

	var local *File
	if repoRoot != "" {
		local, err = ReadFile(LocalPath(repoRoot))
		if err != nil {
			return nil, err
		}
	}

	r := &Resolved{
		Global: global,
		Local:  local,
	}

	// --- identity ---
	// Walk global → local → env, each one overwriting
	if global != nil && global.Identity.Default != "" {
		r.Identity = global.Identity.Default
		r.IdentitySrc = SourceGlobal
	}
	if local != nil && local.Identity.Default != "" {
		r.Identity = local.Identity.Default
		r.IdentitySrc = SourceLocal
	}
	if v := os.Getenv("DTREE_AS"); v != "" {
		r.Identity = v
		r.IdentitySrc = SourceEnv
	}
	if r.Identity == "" {
		r.IdentitySrc = SourceDefault
	}

	// --- editor ---
	if global != nil && global.Editor != "" {
		r.Editor = global.Editor
		r.EditorSrc = SourceGlobal
	}
	if local != nil && local.Editor != "" {
		r.Editor = local.Editor
		r.EditorSrc = SourceLocal
	}
	if r.Editor == "" {
		if e := os.Getenv("EDITOR"); e != "" {
			r.Editor = e
			r.EditorSrc = SourceEnv
		} else {
			r.Editor = "vi"
			r.EditorSrc = SourceDefault
		}
	}

	// --- output ---
	if global != nil && global.Output != "" {
		r.Output = global.Output
		r.OutputSrc = SourceGlobal
	}
	if local != nil && local.Output != "" {
		r.Output = local.Output
		r.OutputSrc = SourceLocal
	}
	if r.Output == "" {
		r.Output = "human"
		r.OutputSrc = SourceDefault
	}

	// --- color ---
	if global != nil && global.Color != "" {
		r.Color = global.Color
		r.ColorSrc = SourceGlobal
	}
	if local != nil && local.Color != "" {
		r.Color = local.Color
		r.ColorSrc = SourceLocal
	}
	if r.Color == "" {
		r.Color = "auto"
		r.ColorSrc = SourceDefault
	}

	// --- default_tree ---
	if global != nil && global.DefaultTree != "" {
		r.DefaultTree = global.DefaultTree
		r.DefaultTreeSrc = SourceGlobal
	}
	if local != nil && local.DefaultTree != "" {
		r.DefaultTree = local.DefaultTree
		r.DefaultTreeSrc = SourceLocal
	}
	if v := os.Getenv("DTREE_TREE"); v != "" {
		r.DefaultTree = v
		r.DefaultTreeSrc = SourceEnv
	}
	if r.DefaultTree == "" {
		r.DefaultTreeSrc = SourceDefault
	}

	return r, nil
}

// Get returns the value and source for the given dotted key. Supported keys:
// identity.default, editor, output, color, default_tree.
// Returns ("", "", false) for unknown keys.
func (r *Resolved) Get(key string) (value string, source Source, ok bool) {
	switch key {
	case "identity.default":
		return r.Identity, r.IdentitySrc, true
	case "editor":
		return r.Editor, r.EditorSrc, true
	case "output":
		return r.Output, r.OutputSrc, true
	case "color":
		return r.Color, r.ColorSrc, true
	case "default_tree":
		return r.DefaultTree, r.DefaultTreeSrc, true
	default:
		return "", "", false
	}
}

// Set writes a single key to the config file at the given scope (SourceGlobal
// or SourceLocal). Returns an error for other scopes or for invalid values.
func Set(scope Source, repoRoot, key, value string) error {
	if scope != SourceGlobal && scope != SourceLocal {
		return fmt.Errorf("config: set: invalid scope %q; must be %q or %q", scope, SourceGlobal, SourceLocal)
	}

	// Validate restricted keys.
	switch key {
	case "output":
		if value != "human" && value != "json" && value != "yaml" {
			return fmt.Errorf("config: invalid value for output: %q; must be one of: human, json, yaml", value)
		}
	case "color":
		if value != "auto" && value != "always" && value != "never" {
			return fmt.Errorf("config: invalid value for color: %q; must be one of: auto, always, never", value)
		}
	}

	path, err := scopePath(scope, repoRoot)
	if err != nil {
		return err
	}

	f, err := ReadFile(path)
	if err != nil {
		return err
	}
	if f == nil {
		f = &File{}
	}

	if err := setField(f, key, value); err != nil {
		return err
	}

	return WriteFile(path, f)
}

// Unset clears a single key in the config file at the given scope.
func Unset(scope Source, repoRoot, key string) error {
	if scope != SourceGlobal && scope != SourceLocal {
		return fmt.Errorf("config: unset: invalid scope %q; must be %q or %q", scope, SourceGlobal, SourceLocal)
	}

	path, err := scopePath(scope, repoRoot)
	if err != nil {
		return err
	}

	f, err := ReadFile(path)
	if err != nil {
		return err
	}
	if f == nil {
		// Nothing to unset.
		return nil
	}

	if err := setField(f, key, ""); err != nil {
		return err
	}

	return WriteFile(path, f)
}

// List returns all resolved config values keyed by dotted path.
// Useful for `dtree config list`.
func List(repoRoot string) (map[string]string, error) {
	r, err := Load(repoRoot)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"identity.default": r.Identity,
		"editor":           r.Editor,
		"output":           r.Output,
		"color":            r.Color,
		"default_tree":     r.DefaultTree,
	}, nil
}

// IdentityCatalog returns the merged list of identities from the global
// config. Used by the UI's identity dropdown filtering.
func IdentityCatalog(r *Resolved) []core.Actor {
	if r.Global == nil {
		return nil
	}
	return r.Global.Identity.Identities
}

// scopePath resolves the file path for the given scope.
func scopePath(scope Source, repoRoot string) (string, error) {
	switch scope {
	case SourceGlobal:
		return GlobalPath()
	case SourceLocal:
		if repoRoot == "" {
			return "", fmt.Errorf("config: local scope requires a non-empty repoRoot")
		}
		return LocalPath(repoRoot), nil
	default:
		return "", fmt.Errorf("config: unknown scope %q", scope)
	}
}

// setField sets a single named field on f.
func setField(f *File, key, value string) error {
	switch key {
	case "identity.default":
		f.Identity.Default = value
	case "editor":
		f.Editor = value
	case "output":
		f.Output = value
	case "color":
		f.Color = value
	case "default_tree":
		f.DefaultTree = value
	default:
		return fmt.Errorf("config: unknown key %q", key)
	}
	return nil
}

// marshal encodes f as YAML with 2-space indentation, matching the rest of
// the dtree storage layer.
func marshal(f *File) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(f); err != nil {
		_ = enc.Close()
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	out := buf.Bytes()
	// Normalize to exactly one trailing newline.
	out = bytes.TrimRight(out, "\n")
	out = append(out, '\n')
	return out, nil
}
