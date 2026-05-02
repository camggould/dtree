// Package storage handles on-disk persistence of decisions, trees,
// actors, and configuration as YAML files. Decisions are the source
// of truth for current state; tree.yaml and actors.yaml are the
// project-level registries.
//
// Writes are atomic (tmp+fsync+rename via fsutil) so readers never
// see torn files. Validation runs before every write so we don't
// produce something we couldn't read back.
package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/fsutil"
	"gopkg.in/yaml.v3"
)

const (
	// FileMode is the permissions bits for all decision/tree/actor YAML
	// files: world-readable, owner-writable. Standard project file mode.
	FileMode os.FileMode = 0o644

	// ActorsFileName is the per-project actor registry, relative to .decisions/.
	ActorsFileName = "actors.yaml"

	// TreesFileName is the per-project tree registry index.
	TreesFileName = "trees.yaml"

	// TreeMetaFileName is the per-tree metadata file (inside <tree>/).
	TreeMetaFileName = "tree.yaml"

	// ConfigFileName is the project-local config (inside .decisions/).
	ConfigFileName = "config.yaml"
)

// ReadDecision loads and unmarshals a decision YAML at path. It does
// not enforce domain invariants — that's the validation layer's job —
// but it does ensure the YAML is syntactically valid and that all
// required scalar fields are present.
func ReadDecision(path string) (*core.Decision, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("storage: read decision %s: %w", path, err)
	}
	var d core.Decision
	if err := yaml.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("storage: parse decision %s: %w", path, err)
	}
	if d.SchemaVersion == 0 {
		d.SchemaVersion = core.SchemaVersion
	}
	// Tree slug is derived from path; the YAML doesn't carry it
	// (would be redundant with directory location and could drift).
	d.Tree = treeFromDecisionPath(path)
	return &d, nil
}

// WriteDecision marshals d as YAML and atomically replaces path.
// If path is empty, the caller is expected to have set it via convention
// (see DecisionPath).
func WriteDecision(path string, d *core.Decision) error {
	if d.SchemaVersion == 0 {
		d.SchemaVersion = core.SchemaVersion
	}
	data, err := marshal(d)
	if err != nil {
		return fmt.Errorf("storage: marshal decision: %w", err)
	}
	if err := fsutil.WriteAtomic(path, data, FileMode); err != nil {
		return fmt.Errorf("storage: write decision: %w", err)
	}
	return nil
}

// DecisionPath builds the canonical filename for a decision under a
// per-tree decisions directory: <ULID>-<slug>.yaml.
func DecisionPath(treeDir, id, slug string) string {
	name := id + "-" + slug + ".yaml"
	return filepath.Join(treeDir, "decisions", name)
}

// treeFromDecisionPath extracts the tree slug from a path of the shape
// .../<tree>/decisions/<file>. Returns "" if the path doesn't match.
func treeFromDecisionPath(path string) string {
	dir := filepath.Dir(path) // .../<tree>/decisions
	parent := filepath.Dir(dir)
	if filepath.Base(dir) != "decisions" {
		return ""
	}
	return filepath.Base(parent)
}

// ReadTree loads a tree.yaml.
func ReadTree(path string) (*core.Tree, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("storage: read tree %s: %w", path, err)
	}
	var t core.Tree
	if err := yaml.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("storage: parse tree %s: %w", path, err)
	}
	if t.SchemaVersion == 0 {
		t.SchemaVersion = core.SchemaVersion
	}
	return &t, nil
}

// WriteTree atomically writes a tree.yaml.
func WriteTree(path string, t *core.Tree) error {
	if t.SchemaVersion == 0 {
		t.SchemaVersion = core.SchemaVersion
	}
	data, err := marshal(t)
	if err != nil {
		return fmt.Errorf("storage: marshal tree: %w", err)
	}
	return fsutil.WriteAtomic(path, data, FileMode)
}

// ActorsFile is the on-disk shape of actors.yaml — a wrapper struct
// with schema_version and an actors list.
type ActorsFile struct {
	SchemaVersion int          `yaml:"schema_version"`
	Actors        []core.Actor `yaml:"actors"`
}

// ReadActors loads actors.yaml.
func ReadActors(path string) (*ActorsFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("storage: read actors %s: %w", path, err)
	}
	var af ActorsFile
	if err := yaml.Unmarshal(data, &af); err != nil {
		return nil, fmt.Errorf("storage: parse actors %s: %w", path, err)
	}
	if af.SchemaVersion == 0 {
		af.SchemaVersion = core.SchemaVersion
	}
	return &af, nil
}

// WriteActors atomically writes actors.yaml.
func WriteActors(path string, af *ActorsFile) error {
	if af.SchemaVersion == 0 {
		af.SchemaVersion = core.SchemaVersion
	}
	data, err := marshal(af)
	if err != nil {
		return fmt.Errorf("storage: marshal actors: %w", err)
	}
	return fsutil.WriteAtomic(path, data, FileMode)
}

// TreesFile is the on-disk shape of trees.yaml.
type TreesFile struct {
	SchemaVersion int      `yaml:"schema_version"`
	Trees         []string `yaml:"trees"` // slugs
}

// ReadTrees loads the project tree registry.
func ReadTrees(path string) (*TreesFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("storage: read trees %s: %w", path, err)
	}
	var tf TreesFile
	if err := yaml.Unmarshal(data, &tf); err != nil {
		return nil, fmt.Errorf("storage: parse trees %s: %w", path, err)
	}
	if tf.SchemaVersion == 0 {
		tf.SchemaVersion = core.SchemaVersion
	}
	return &tf, nil
}

// WriteTrees atomically writes trees.yaml.
func WriteTrees(path string, tf *TreesFile) error {
	if tf.SchemaVersion == 0 {
		tf.SchemaVersion = core.SchemaVersion
	}
	data, err := marshal(tf)
	if err != nil {
		return fmt.Errorf("storage: marshal trees: %w", err)
	}
	return fsutil.WriteAtomic(path, data, FileMode)
}

// marshal emits YAML with consistent formatting: 2-space indent,
// no trailing whitespace, deterministic key ordering (struct field order).
// Long string fields (description, etc.) emit as block scalars when
// they contain newlines — gopkg.in/yaml.v3 handles this automatically.
func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		_ = enc.Close()
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	out := buf.Bytes()
	// Strip a trailing extra newline if the encoder added one.
	out = bytes.TrimRight(out, "\n")
	out = append(out, '\n')
	return out, nil
}

// MarshalJSON exists to convert a decision to a JSON-friendly form
// preserving the canonical field naming used by the HTTP API. This is
// a thin convenience around encoding/json with the struct's json tags.
func MarshalDecisionJSON(d *core.Decision) ([]byte, error) {
	out, err := json.Marshal(d)
	if err != nil {
		return nil, fmt.Errorf("storage: marshal decision json: %w", err)
	}
	return out, nil
}

// SlugFromSummary derives a URL/filename-friendly slug from a free-form
// summary. It lowercases, replaces non-alphanum with single hyphens,
// collapses runs of hyphens, and trims to 80 chars at a word boundary.
//
// Slugs are informational (filename hints); the canonical ID is the ULID.
func SlugFromSummary(summary string) string {
	const max = 80
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(summary) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case prevDash:
			// already wrote a dash; skip
		default:
			if b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
		if b.Len() >= max {
			break
		}
	}
	s := strings.TrimRight(b.String(), "-")
	if s == "" {
		return "decision"
	}
	return s
}
