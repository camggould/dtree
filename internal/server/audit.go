package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/core"
)

// auditListResponse is the JSON body for GET /v1/audit.
type auditListResponse struct {
	Events     []core.Event `json:"events"`
	NextCursor *string      `json:"next_cursor"`
}

// auditHandlers bundles handlers that share the repoRoot dependency.
type auditHandlers struct {
	repoRoot string
}

// newAuditHandlers constructs an auditHandlers for the given repo root.
func newAuditHandlers(repoRoot string) *auditHandlers {
	return &auditHandlers{repoRoot: repoRoot}
}

// parseRelativeDuration parses a relative duration string like "7d", "24h", "30m"
// or falls back to RFC3339. Returns zero time on empty input.
func parseRelativeDuration(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	// Try RFC3339 first.
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	// Try relative: <N><unit> where unit in d, h, m, s.
	if len(s) < 2 {
		return time.Time{}, fmt.Errorf("unparseable time %q", s)
	}
	unit := s[len(s)-1]
	n, err := strconv.ParseFloat(s[:len(s)-1], 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("unparseable time %q", s)
	}
	var d time.Duration
	switch unit {
	case 'd':
		d = time.Duration(n * float64(24*time.Hour))
	case 'h':
		d = time.Duration(n * float64(time.Hour))
	case 'm':
		d = time.Duration(n * float64(time.Minute))
	case 's':
		d = time.Duration(n * float64(time.Second))
	default:
		return time.Time{}, fmt.Errorf("unparseable time %q", s)
	}
	return time.Now().UTC().Add(-d), nil
}

// buildFilter constructs an audit.Filter from query parameters.
// It does NOT apply the limit or cursor to the filter (those are handled at
// the pagination layer so we can detect "more pages exist").
// Returns: filter, cursor, limit, descending?, error.
func buildFilter(r *http.Request) (audit.Filter, string, int, bool, error) {
	q := r.URL.Query()

	var f audit.Filter
	f.Tree = q.Get("tree")
	f.Actor = q.Get("actor")
	if a := q.Get("action"); a != "" {
		f.Action = core.Action(a)
	}
	if k := q.Get("kind"); k != "" {
		f.Kind = core.Kind(k)
	}
	f.TargetID = q.Get("decision")

	var err error
	f.Since, err = parseRelativeDuration(q.Get("since"))
	if err != nil {
		return audit.Filter{}, "", 0, false, fmt.Errorf("since: %w", err)
	}
	f.Until, err = parseRelativeDuration(q.Get("until"))
	if err != nil {
		return audit.Filter{}, "", 0, false, fmt.Errorf("until: %w", err)
	}

	limit := 50
	if lStr := q.Get("limit"); lStr != "" {
		limit, err = strconv.Atoi(lStr)
		if err != nil || limit < 1 {
			return audit.Filter{}, "", 0, false, fmt.Errorf("limit must be a positive integer")
		}
		if limit > 1000 {
			limit = 1000
		}
	}

	desc := false
	switch q.Get("order") {
	case "", "asc":
		// default: oldest first
	case "desc":
		desc = true
	default:
		return audit.Filter{}, "", 0, false, fmt.Errorf("order must be 'asc' or 'desc'")
	}

	cursor := q.Get("cursor")
	return f, cursor, limit, desc, nil
}

// auditList handles GET /v1/audit.
func (h *auditHandlers) auditList(w http.ResponseWriter, r *http.Request) {
	f, cursor, limit, desc, err := buildFilter(r)
	if err != nil {
		WriteProblem(w, r, BadRequest(err.Error()))
		return
	}

	// Fetch all matching events (no limit; we paginate manually).
	events, err := audit.Read(h.repoRoot, f)
	if err != nil {
		WriteProblem(w, r, Internal("failed to read audit log"))
		return
	}

	// audit.Read returns ascending by (ts, event_id). Reverse for desc so
	// pagination semantics stay symmetric: cursor always means "give me
	// items strictly after this position in the current order."
	if desc {
		for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
			events[i], events[j] = events[j], events[i]
		}
	}

	// Apply cursor: skip events at-or-before the cursor in the active order.
	// asc:  skip event_id <= cursor    (move forward in time)
	// desc: skip event_id >= cursor    (move backward in time)
	if cursor != "" {
		start := 0
		for start < len(events) {
			id := events[start].EventID
			if (!desc && id <= cursor) || (desc && id >= cursor) {
				start++
				continue
			}
			break
		}
		events = events[start:]
	}

	// Determine if there's a next page.
	var nextCursor *string
	if len(events) > limit {
		last := events[limit-1].EventID
		nextCursor = &last
		events = events[:limit]
	}

	resp := auditListResponse{
		Events:     events,
		NextCursor: nextCursor,
	}
	if resp.Events == nil {
		resp.Events = []core.Event{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(resp)
}

// auditShow handles GET /v1/audit/{event_id}.
func (h *auditHandlers) auditShow(w http.ResponseWriter, r *http.Request) {
	eventID := chi.URLParam(r, "event_id")

	events, err := audit.Read(h.repoRoot, audit.Filter{})
	if err != nil {
		WriteProblem(w, r, Internal("failed to read audit log"))
		return
	}

	for _, ev := range events {
		if ev.EventID == eventID {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			enc := json.NewEncoder(w)
			enc.SetEscapeHTML(false)
			_ = enc.Encode(ev)
			return
		}
	}

	WriteProblem(w, r, NotFound(fmt.Sprintf("event %q not found", eventID)))
}

// auditStream handles GET /v1/audit/stream (Server-Sent Events).
func (h *auditHandlers) auditStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteProblem(w, r, Internal("streaming not supported"))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Determine initial cursor from ?since= query param (treated as a ULID).
	lastSeen := r.URL.Query().Get("since")

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			events, err := audit.Read(h.repoRoot, audit.Filter{})
			if err != nil {
				// Log and continue; don't crash the stream.
				continue
			}

			for _, ev := range events {
				if ev.EventID <= lastSeen {
					continue
				}
				data, err := json.Marshal(ev)
				if err != nil {
					continue
				}
				fmt.Fprintf(w, "event: audit\ndata: %s\n\n", data)
				lastSeen = ev.EventID
			}
			flusher.Flush()
		}
	}
}

// collectJSONLPaths returns all *.jsonl file paths under the audit directories,
// sorted by filename (chronological because filenames are YYYY-MM.jsonl).
func collectJSONLPaths(repoRoot string) ([]string, error) {
	var paths []string

	// Repo-level: .decisions/audit/*.jsonl
	repoAuditDir := filepath.Join(repoRoot, ".decisions", "audit")
	repoFiles, err := filepath.Glob(filepath.Join(repoAuditDir, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	paths = append(paths, repoFiles...)

	// Per-tree: .decisions/<tree>/audit/*.jsonl
	decisionsDir := filepath.Join(repoRoot, ".decisions")
	entries, err := os.ReadDir(decisionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			// Nothing to export.
			sort.Strings(paths)
			return paths, nil
		}
		return nil, fmt.Errorf("export: read decisions dir: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "audit" {
			continue
		}
		treeAuditDir := filepath.Join(decisionsDir, e.Name(), "audit")
		treeFiles, err := filepath.Glob(filepath.Join(treeAuditDir, "*.jsonl"))
		if err != nil {
			return nil, err
		}
		paths = append(paths, treeFiles...)
	}

	sort.Strings(paths)
	return paths, nil
}

// auditExport handles GET /v1/audit/export.
func (h *auditHandlers) auditExport(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format != "jsonl" {
		WriteProblem(w, r, BadRequest(fmt.Sprintf("unsupported format %q; only format=jsonl is supported", format)))
		return
	}

	paths, err := collectJSONLPaths(h.repoRoot)
	if err != nil {
		WriteProblem(w, r, Internal("failed to collect audit files"))
		return
	}

	w.Header().Set("Content-Type", "application/jsonl; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			// Already wrote 200; best effort.
			return
		}
		_, _ = io.Copy(w, f)
		f.Close()
	}
}

// mountAuditRoutes registers all /v1/audit routes on the given chi.Router.
// The /v1/audit/stream and /v1/audit/export routes must be registered before
// /v1/audit/{event_id} so chi matches them first.
func mountAuditRoutes(r chi.Router, h *auditHandlers) {
	r.Route("/audit", func(r chi.Router) {
		r.Get("/", h.auditList)
		r.Get("/stream", h.auditStream)
		r.Get("/export", h.auditExport)
		r.Get("/{event_id}", h.auditShow)
	})
}

