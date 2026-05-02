// Package audit implements the dtree append-only audit log: JSONL append,
// multi-file read with filtering, and point-in-time state replay.
//
// Events are partitioned into monthly JSONL files:
//   - repo-level:  <repoRoot>/.decisions/audit/YYYY-MM.jsonl
//   - per-tree:    <repoRoot>/.decisions/<tree>/audit/YYYY-MM.jsonl
//
// Writes use O_APPEND which the kernel makes atomic for payloads well under
// PIPE_BUF (~4 KB on Linux), so concurrent writers never interleave lines.
package audit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/fsutil"
	"github.com/cgould/dtree/internal/ulid"
)

// Filter constrains which events Read returns. The zero value matches all
// events (useful for "dump everything"). All non-zero fields are ANDed.
type Filter struct {
	// Tree, if non-empty, limits results to events from that tree slug.
	// Repo-level events (Tree=="") are excluded when this is set.
	Tree string
	// Actor, if non-empty, matches only events with that actor handle.
	Actor string
	// Action, if non-empty, matches only events with that action.
	Action core.Action
	// Kind, if non-empty, matches only events with that kind.
	Kind core.Kind
	// TargetID, if non-empty, matches only events whose ID field equals it.
	TargetID string
	// Since, if non-zero, matches only events with Ts >= Since.
	Since time.Time
	// Until, if non-zero, matches only events with Ts <= Until.
	Until time.Time
	// Limit is the maximum number of events to return (0 = unlimited).
	Limit int
}

// auditDir returns the audit directory for the given tree slug. If tree is
// empty, returns the repo-level audit directory.
func auditDir(repoRoot, tree string) string {
	if tree == "" {
		return filepath.Join(repoRoot, ".decisions", "audit")
	}
	return filepath.Join(repoRoot, ".decisions", tree, "audit")
}

// monthFile returns the JSONL file path for the month of t.
func monthFile(dir string, t time.Time) string {
	return filepath.Join(dir, t.UTC().Format("2006-01")+".jsonl")
}

// Append writes ev as a JSON line to the appropriate monthly JSONL file.
// Fields that are zero/empty are filled with defaults before writing:
//   - V set to core.SchemaVersion if zero
//   - EventID set to a new ULID if empty
//   - Ts set to time.Now().UTC() if zero
//
// Caller-supplied non-zero values are always preserved.
func Append(repoRoot string, ev core.Event) error {
	if ev.V == 0 {
		ev.V = core.SchemaVersion
	}
	if ev.EventID == "" {
		ev.EventID = ulid.New()
	}
	if ev.Ts.IsZero() {
		ev.Ts = time.Now().UTC()
	}

	dir := auditDir(repoRoot, ev.Tree)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("audit: mkdir %s: %w", dir, err)
	}

	line, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("audit: marshal event: %w", err)
	}

	path := monthFile(dir, ev.Ts)
	if err := fsutil.AppendLine(path, line, 0o644); err != nil {
		return fmt.Errorf("audit: append to %s: %w", path, err)
	}
	return nil
}

// Read collects events matching f from all relevant audit directories,
// returning them sorted by (Ts ASC, EventID ASC). Missing audit directories
// are silently ignored; only unexpected I/O errors are returned.
func Read(repoRoot string, f Filter) ([]core.Event, error) {
	var dirs []string
	if f.Tree != "" {
		// Only this tree's audit dir.
		dirs = []string{auditDir(repoRoot, f.Tree)}
	} else {
		// Repo-level plus all per-tree audit dirs.
		dirs = append(dirs, auditDir(repoRoot, ""))
		treeDirs, err := discoverTreeAuditDirs(repoRoot)
		if err != nil {
			return nil, err
		}
		dirs = append(dirs, treeDirs...)
	}

	var events []core.Event
	for _, dir := range dirs {
		evs, err := readDir(dir, f)
		if err != nil {
			return nil, err
		}
		events = append(events, evs...)
	}

	sort.Slice(events, func(i, j int) bool {
		ti, tj := events[i].Ts, events[j].Ts
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		return events[i].EventID < events[j].EventID
	})

	if f.Limit > 0 && len(events) > f.Limit {
		events = events[:f.Limit]
	}
	return events, nil
}

// discoverTreeAuditDirs returns the audit/ subdirectory for every tree that
// has one under .decisions/.
func discoverTreeAuditDirs(repoRoot string) ([]string, error) {
	base := filepath.Join(repoRoot, ".decisions")
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("audit: read decisions dir: %w", err)
	}
	var dirs []string
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "audit" {
			continue
		}
		d := filepath.Join(base, e.Name(), "audit")
		dirs = append(dirs, d)
	}
	return dirs, nil
}

// readDir reads all *.jsonl files from dir, applying f's predicates. It
// silently returns empty when the directory does not exist.
func readDir(dir string, f Filter) ([]core.Event, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("audit: read dir %s: %w", dir, err)
	}

	var events []core.Event
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		evs, err := readFile(filepath.Join(dir, e.Name()), f)
		if err != nil {
			return nil, err
		}
		events = append(events, evs...)
	}
	return events, nil
}

// readFile decodes each JSON line in path into an Event, applying f's
// predicates. Lines that fail to parse are skipped (defensive: audit files
// must survive partial corruption).
func readFile(path string, f Filter) ([]core.Event, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("audit: open %s: %w", path, err)
	}
	defer file.Close()

	var events []core.Event
	sc := bufio.NewScanner(file)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev core.Event
		if err := json.Unmarshal(line, &ev); err != nil {
			// Tolerate malformed lines; don't abort the whole read.
			continue
		}
		if matchesFilter(ev, f) {
			events = append(events, ev)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("audit: scan %s: %w", path, err)
	}
	return events, nil
}

// matchesFilter reports whether ev satisfies all non-zero predicates in f.
func matchesFilter(ev core.Event, f Filter) bool {
	if f.Tree != "" && ev.Tree != f.Tree {
		return false
	}
	if f.Actor != "" && ev.Actor != f.Actor {
		return false
	}
	if f.Action != "" && ev.Action != f.Action {
		return false
	}
	if f.Kind != "" && ev.Kind != f.Kind {
		return false
	}
	if f.TargetID != "" && ev.ID != f.TargetID {
		return false
	}
	if !f.Since.IsZero() && ev.Ts.Before(f.Since) {
		return false
	}
	if !f.Until.IsZero() && ev.Ts.After(f.Until) {
		return false
	}
	return true
}

// ReplayState reconstructs the set of decisions in tree as they existed at
// the point in time at (inclusive). It replays audit events in chronological
// order, applying each action to an in-memory map of decisions.
//
// Returns a map keyed by decision ID. Decisions that were deleted by the
// cutoff are absent from the map.
//
// ReplayState only considers events where Kind == KindDecision (or
// KindRelationship for relate/unrelate). Tree-level and actor-level events
// are ignored.
func ReplayState(repoRoot, tree string, at time.Time) (map[string]*core.Decision, error) {
	f := Filter{
		Tree:  tree,
		Until: at,
	}
	events, err := Read(repoRoot, f)
	if err != nil {
		return nil, fmt.Errorf("audit: replay %s@%s: %w", tree, at.Format(time.RFC3339), err)
	}

	state := make(map[string]*core.Decision)
	for _, ev := range events {
		applyEvent(state, ev)
	}
	return state, nil
}

// applyEvent mutates state according to ev's action.
func applyEvent(state map[string]*core.Decision, ev core.Event) {
	id := ev.ID
	switch ev.Action {
	case core.ActionCreate, core.ActionExternalCreate:
		d := decisionFromAfter(ev.Payload.After)
		if d == nil {
			d = &core.Decision{}
		}
		d.ID = id
		if ev.Tree != "" {
			d.Tree = ev.Tree
		}
		state[id] = d

	case core.ActionUpdate, core.ActionExternalEdit:
		d, ok := state[id]
		if !ok {
			// Update without a prior create; materialise a shell.
			d = &core.Decision{ID: id, Tree: ev.Tree}
			state[id] = d
		}
		applyAfterFields(d, ev.Payload.After)

	case core.ActionDelete, core.ActionExternalDelete:
		delete(state, id)

	case core.ActionDecide:
		d, ok := state[id]
		if !ok {
			break
		}
		d.Status = core.StatusDecided
		// decide payload uses Extra for its specific fields.
		extra := ev.Payload.Extra
		if extra == nil {
			extra = ev.Payload.After
		}
		if extra != nil {
			if v, ok := extra["actual_choice"].(string); ok {
				d.ActualChoice = v
			}
			if v, ok := extra["actual_choice_reason"].(string); ok {
				d.ActualChoiceReason = v
			}
			if v, ok := extra["is_recommended"].(bool); ok {
				d.IsRecommended = v
			}
			if v, ok := extra["decided_by"]; ok {
				d.DecidedBy = toStringSlice(v)
			}
		}

	case core.ActionUndecide:
		d, ok := state[id]
		if !ok {
			break
		}
		d.Status = core.StatusProposed
		d.ActualChoice = ""
		d.ActualChoiceReason = ""
		d.DecidedBy = nil
		d.IsRecommended = false

	case core.ActionScopeOut:
		d, ok := state[id]
		if !ok {
			break
		}
		d.Status = core.StatusOutOfScope
		extra := ev.Payload.Extra
		if extra == nil {
			extra = ev.Payload.After
		}
		if extra != nil {
			if v, ok := extra["out_of_scope_reason"].(string); ok {
				d.OutOfScopeReason = v
			}
		}

	case core.ActionSupersede:
		d, ok := state[id]
		if !ok {
			break
		}
		d.Status = core.StatusSuperseded

	case core.ActionRelate:
		d, ok := state[id]
		if !ok {
			break
		}
		extra := ev.Payload.Extra
		if extra == nil {
			break
		}
		relType, _ := extra["type"].(string)
		target, _ := extra["target"].(string)
		if relType != "" && target != "" {
			d.Relationships = append(d.Relationships, core.Relationship{
				Type:   core.RelationshipType(relType),
				Target: target,
			})
		}

	case core.ActionUnrelate:
		d, ok := state[id]
		if !ok {
			break
		}
		extra := ev.Payload.Extra
		if extra == nil {
			break
		}
		relType, _ := extra["type"].(string)
		target, _ := extra["target"].(string)
		rels := d.Relationships[:0]
		for _, r := range d.Relationships {
			if string(r.Type) == relType && r.Target == target {
				continue
			}
			rels = append(rels, r)
		}
		d.Relationships = rels

	case core.ActionRename:
		d, ok := state[id]
		if !ok {
			break
		}
		extra := ev.Payload.Extra
		if extra == nil {
			extra = ev.Payload.After
		}
		if extra != nil {
			if v, ok := extra["slug"].(string); ok {
				d.Slug = v
			}
			if v, ok := extra["summary"].(string); ok {
				d.Summary = v
			}
		}

	case core.ActionRestore:
		// restore brings back a deleted decision; payload.After has full state.
		d := decisionFromAfter(ev.Payload.After)
		if d == nil {
			d = &core.Decision{}
		}
		d.ID = id
		if ev.Tree != "" {
			d.Tree = ev.Tree
		}
		state[id] = d
	}
}

// decisionFromAfter constructs a Decision from an after map. Returns nil
// when after is nil or empty.
func decisionFromAfter(after map[string]any) *core.Decision {
	if len(after) == 0 {
		return nil
	}
	d := &core.Decision{}
	applyAfterFields(d, after)
	return d
}

// applyAfterFields merges non-zero values from after into d. Only the fields
// that are present in the map are updated; absent keys leave d unchanged.
func applyAfterFields(d *core.Decision, after map[string]any) {
	if after == nil {
		return
	}
	setString := func(field *string, key string) {
		if v, ok := after[key].(string); ok {
			*field = v
		}
	}
	setBool := func(field *bool, key string) {
		if v, ok := after[key].(bool); ok {
			*field = v
		}
	}

	setString(&d.ID, "id")
	setString(&d.Slug, "slug")
	setString(&d.Tree, "tree")
	setString(&d.Summary, "summary")
	setString(&d.Creator, "creator")
	setString(&d.Assignee, "assignee")
	setString(&d.Description, "decision_full_description")
	setString(&d.RecommendedSummary, "recommended_summary")
	setString(&d.RecommendedFull, "recommended_full")
	setString(&d.RecommendedBy, "recommended_by")
	setString(&d.ActualChoice, "actual_choice")
	setString(&d.ActualChoiceReason, "actual_choice_reason")
	setString(&d.OutOfScopeReason, "out_of_scope_reason")
	setBool(&d.IsRecommended, "is_recommended")

	if v, ok := after["priority"].(string); ok {
		d.Priority = core.Priority(v)
	}
	if v, ok := after["status"].(string); ok {
		d.Status = core.Status(v)
	}
	if v, ok := after["schema_version"]; ok {
		switch n := v.(type) {
		case float64:
			d.SchemaVersion = int(n)
		case int:
			d.SchemaVersion = n
		}
	}
	if v, ok := after["tags"]; ok {
		d.Tags = toStringSlice(v)
	}
	if v, ok := after["decided_by"]; ok {
		d.DecidedBy = toStringSlice(v)
	}
	if v, ok := after["relationships"]; ok {
		d.Relationships = toRelationships(v)
	}
}

// toStringSlice converts a JSON-decoded []interface{} to []string.
func toStringSlice(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// toRelationships converts a JSON-decoded []interface{} to []Relationship.
func toRelationships(v any) []core.Relationship {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]core.Relationship, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		var r core.Relationship
		if t, ok := m["type"].(string); ok {
			r.Type = core.RelationshipType(t)
		}
		if tgt, ok := m["target"].(string); ok {
			r.Target = tgt
		}
		if r.Type != "" && r.Target != "" {
			out = append(out, r)
		}
	}
	return out
}

