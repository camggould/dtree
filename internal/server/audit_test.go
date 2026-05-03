//go:build sqlite_fts5

package server_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/server"
)

// appendEvent is a test helper that appends a synthetic event to the audit log
// and returns it (with defaults filled in).
func appendEvent(t *testing.T, repoRoot string, ev core.Event) core.Event {
	t.Helper()
	if ev.V == 0 {
		ev.V = core.SchemaVersion
	}
	if ev.Actor == "" {
		ev.Actor = "cam"
	}
	if ev.Action == "" {
		ev.Action = core.ActionCreate
	}
	if ev.Kind == "" {
		ev.Kind = core.KindDecision
	}
	if ev.ID == "" {
		ev.ID = fmt.Sprintf("00000000000000000000000%03d", 1)
	}
	if err := audit.Append(repoRoot, ev); err != nil {
		t.Fatalf("appendEvent: %v", err)
	}
	// Re-read to get the filled-in EventID and Ts.
	events, err := audit.Read(repoRoot, audit.Filter{})
	if err != nil {
		t.Fatalf("appendEvent: read: %v", err)
	}
	// Return the last event (most recently appended by Ts/EventID sort is not guaranteed
	// since we may have many events; find by matching Actor+Action+Kind+ID).
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Actor == ev.Actor && events[i].Action == ev.Action &&
			events[i].Kind == ev.Kind && events[i].ID == ev.ID {
			return events[i]
		}
	}
	return ev
}

// makeAuditServer returns a test server with audit routes mounted and the repo root.
func makeAuditServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	cfg := testConfig(t)
	srv := server.New(cfg)
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)
	return ts, cfg.RepoRoot
}

// ---- TestAuditListEmpty ----

func TestAuditListEmpty(t *testing.T) {
	cfg := testConfig(t)
	srv := server.New(cfg)

	req := httptest.NewRequest(http.MethodGet, "/v1/audit", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", w.Code)
	}

	var resp struct {
		Events     []core.Event `json:"events"`
		NextCursor *string      `json:"next_cursor"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Events) != 0 {
		t.Errorf("events = %d; want 0", len(resp.Events))
	}
	if resp.NextCursor != nil {
		t.Errorf("next_cursor = %v; want nil", resp.NextCursor)
	}
}

// ---- TestAuditListAll ----

func TestAuditListAll(t *testing.T) {
	cfg := testConfig(t)
	srv := server.New(cfg)
	repo := cfg.RepoRoot

	for i := 0; i < 3; i++ {
		appendEvent(t, repo, core.Event{
			Actor:  "cam",
			Action: core.ActionCreate,
			Kind:   core.KindDecision,
			ID:     fmt.Sprintf("00000000000000000000000%03d", i+1),
		})
		time.Sleep(2 * time.Millisecond) // ensure distinct Ts
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/audit", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", w.Code)
	}

	var resp struct {
		Events     []core.Event `json:"events"`
		NextCursor *string      `json:"next_cursor"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Events) != 3 {
		t.Errorf("events = %d; want 3", len(resp.Events))
	}
	if resp.NextCursor != nil {
		t.Errorf("next_cursor should be nil for fewer than default limit events")
	}
}

// ---- Filter tests ----

func TestAuditListFilterByActor(t *testing.T) {
	cfg := testConfig(t)
	srv := server.New(cfg)
	repo := cfg.RepoRoot

	appendEvent(t, repo, core.Event{Actor: "cam", Action: core.ActionCreate, Kind: core.KindDecision, ID: "00000000000000000000000001"})
	appendEvent(t, repo, core.Event{Actor: "bot", Action: core.ActionUpdate, Kind: core.KindDecision, ID: "00000000000000000000000001"})

	req := httptest.NewRequest(http.MethodGet, "/v1/audit?actor=cam", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", w.Code)
	}

	var resp struct {
		Events []core.Event `json:"events"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, ev := range resp.Events {
		if ev.Actor != "cam" {
			t.Errorf("event actor = %q; want cam", ev.Actor)
		}
	}
	if len(resp.Events) == 0 {
		t.Error("expected at least 1 event with actor=cam")
	}
}

func TestAuditListFilterByAction(t *testing.T) {
	cfg := testConfig(t)
	srv := server.New(cfg)
	repo := cfg.RepoRoot

	appendEvent(t, repo, core.Event{Actor: "cam", Action: core.ActionCreate, Kind: core.KindDecision, ID: "00000000000000000000000001"})
	appendEvent(t, repo, core.Event{Actor: "cam", Action: core.ActionUpdate, Kind: core.KindDecision, ID: "00000000000000000000000001"})

	req := httptest.NewRequest(http.MethodGet, "/v1/audit?action=update", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", w.Code)
	}
	var resp struct {
		Events []core.Event `json:"events"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, ev := range resp.Events {
		if ev.Action != core.ActionUpdate {
			t.Errorf("event action = %q; want update", ev.Action)
		}
	}
	if len(resp.Events) == 0 {
		t.Error("expected at least 1 event with action=update")
	}
}

func TestAuditListFilterByDecision(t *testing.T) {
	cfg := testConfig(t)
	srv := server.New(cfg)
	repo := cfg.RepoRoot

	appendEvent(t, repo, core.Event{Actor: "cam", Action: core.ActionCreate, Kind: core.KindDecision, ID: "AAAAAAAAAAAAAAAAAAAAAAAAAA"})
	appendEvent(t, repo, core.Event{Actor: "cam", Action: core.ActionCreate, Kind: core.KindDecision, ID: "BBBBBBBBBBBBBBBBBBBBBBBBBB"})

	req := httptest.NewRequest(http.MethodGet, "/v1/audit?decision=AAAAAAAAAAAAAAAAAAAAAAAAAA", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", w.Code)
	}
	var resp struct {
		Events []core.Event `json:"events"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, ev := range resp.Events {
		if ev.ID != "AAAAAAAAAAAAAAAAAAAAAAAAAA" {
			t.Errorf("event ID = %q; want AAAAAAAAAAAAAAAAAAAAAAAAAA", ev.ID)
		}
	}
	if len(resp.Events) == 0 {
		t.Error("expected at least 1 event matching decision filter")
	}
}

func TestAuditListFilterByTree(t *testing.T) {
	cfg := testConfig(t)
	srv := server.New(cfg)
	repo := cfg.RepoRoot

	appendEvent(t, repo, core.Event{Actor: "cam", Action: core.ActionCreate, Kind: core.KindDecision, Tree: "alpha", ID: "00000000000000000000000001"})
	appendEvent(t, repo, core.Event{Actor: "cam", Action: core.ActionCreate, Kind: core.KindDecision, Tree: "beta", ID: "00000000000000000000000002"})

	req := httptest.NewRequest(http.MethodGet, "/v1/audit?tree=alpha", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", w.Code)
	}
	var resp struct {
		Events []core.Event `json:"events"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, ev := range resp.Events {
		if ev.Tree != "alpha" {
			t.Errorf("event tree = %q; want alpha", ev.Tree)
		}
	}
	if len(resp.Events) == 0 {
		t.Error("expected at least 1 event with tree=alpha")
	}
}

// ---- TestAuditListPagination ----

func TestAuditListPagination(t *testing.T) {
	cfg := testConfig(t)
	srv := server.New(cfg)
	repo := cfg.RepoRoot

	// Append 10 events with small sleeps to get distinct timestamps and ULIDs.
	for i := 0; i < 10; i++ {
		appendEvent(t, repo, core.Event{
			Actor:  "cam",
			Action: core.ActionCreate,
			Kind:   core.KindDecision,
			ID:     fmt.Sprintf("00000000000000000000000%03d", i+1),
		})
		time.Sleep(2 * time.Millisecond)
	}

	// Page 1: limit=3, no cursor.
	req1 := httptest.NewRequest(http.MethodGet, "/v1/audit?limit=3", nil)
	w1 := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Fatalf("page1 status = %d; want 200", w1.Code)
	}

	var page1 struct {
		Events     []core.Event `json:"events"`
		NextCursor *string      `json:"next_cursor"`
	}
	if err := json.NewDecoder(w1.Body).Decode(&page1); err != nil {
		t.Fatalf("page1 decode: %v", err)
	}
	if len(page1.Events) != 3 {
		t.Fatalf("page1 events = %d; want 3", len(page1.Events))
	}
	if page1.NextCursor == nil {
		t.Fatal("page1 next_cursor should not be nil")
	}

	// Page 2: cursor = page1's next_cursor.
	cursor := *page1.NextCursor
	req2 := httptest.NewRequest(http.MethodGet, "/v1/audit?limit=3&cursor="+cursor, nil)
	w2 := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("page2 status = %d; want 200", w2.Code)
	}

	var page2 struct {
		Events     []core.Event `json:"events"`
		NextCursor *string      `json:"next_cursor"`
	}
	if err := json.NewDecoder(w2.Body).Decode(&page2); err != nil {
		t.Fatalf("page2 decode: %v", err)
	}
	if len(page2.Events) != 3 {
		t.Fatalf("page2 events = %d; want 3", len(page2.Events))
	}
	if page2.NextCursor == nil {
		t.Fatal("page2 next_cursor should not be nil (still more pages)")
	}

	// Page 3: cursor = page2's next_cursor.
	cursor2 := *page2.NextCursor
	req3 := httptest.NewRequest(http.MethodGet, "/v1/audit?limit=3&cursor="+cursor2, nil)
	w3 := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w3, req3)

	if w3.Code != http.StatusOK {
		t.Fatalf("page3 status = %d; want 200", w3.Code)
	}

	var page3 struct {
		Events     []core.Event `json:"events"`
		NextCursor *string      `json:"next_cursor"`
	}
	if err := json.NewDecoder(w3.Body).Decode(&page3); err != nil {
		t.Fatalf("page3 decode: %v", err)
	}
	if len(page3.Events) != 3 {
		t.Fatalf("page3 events = %d; want 3", len(page3.Events))
	}

	// Page 4: should return 1 event (total=10, consumed 9 so far), no next_cursor.
	cursor3 := *page3.NextCursor
	req4 := httptest.NewRequest(http.MethodGet, "/v1/audit?limit=3&cursor="+cursor3, nil)
	w4 := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w4, req4)

	if w4.Code != http.StatusOK {
		t.Fatalf("page4 status = %d; want 200", w4.Code)
	}

	var page4 struct {
		Events     []core.Event `json:"events"`
		NextCursor *string      `json:"next_cursor"`
	}
	if err := json.NewDecoder(w4.Body).Decode(&page4); err != nil {
		t.Fatalf("page4 decode: %v", err)
	}
	if len(page4.Events) != 1 {
		t.Fatalf("page4 events = %d; want 1", len(page4.Events))
	}
	if page4.NextCursor != nil {
		t.Errorf("page4 next_cursor = %v; want nil (last page)", page4.NextCursor)
	}

	// Verify no overlap between pages.
	seen := make(map[string]bool)
	for _, page := range [][]core.Event{page1.Events, page2.Events, page3.Events, page4.Events} {
		for _, ev := range page {
			if seen[ev.EventID] {
				t.Errorf("duplicate event_id %q across pages", ev.EventID)
			}
			seen[ev.EventID] = true
		}
	}
	if len(seen) != 10 {
		t.Errorf("total distinct events across pages = %d; want 10", len(seen))
	}
}

// ---- TestAuditListOrderDesc ----

func TestAuditListOrderDesc(t *testing.T) {
	cfg := testConfig(t)
	srv := server.New(cfg)
	repo := cfg.RepoRoot

	for i := 0; i < 5; i++ {
		appendEvent(t, repo, core.Event{
			Actor:  "cam",
			Action: core.ActionCreate,
			Kind:   core.KindDecision,
			ID:     fmt.Sprintf("00000000000000000000000%03d", i+1),
		})
		time.Sleep(2 * time.Millisecond)
	}

	// limit=2&order=desc must return the two newest events, newest first.
	req := httptest.NewRequest(http.MethodGet, "/v1/audit?limit=2&order=desc", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", w.Code)
	}
	var page1 struct {
		Events     []core.Event `json:"events"`
		NextCursor *string      `json:"next_cursor"`
	}
	if err := json.NewDecoder(w.Body).Decode(&page1); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(page1.Events) != 2 {
		t.Fatalf("events = %d; want 2", len(page1.Events))
	}
	if page1.Events[0].Ts.Before(page1.Events[1].Ts) {
		t.Errorf("desc order broken: first event ts %v < second %v",
			page1.Events[0].Ts, page1.Events[1].Ts)
	}
	if page1.NextCursor == nil {
		t.Fatal("next_cursor should be set on first page")
	}

	// Page 2 with desc cursor: should walk further back in time.
	req2 := httptest.NewRequest(http.MethodGet,
		"/v1/audit?limit=2&order=desc&cursor="+*page1.NextCursor, nil)
	w2 := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w2, req2)
	var page2 struct {
		Events     []core.Event `json:"events"`
		NextCursor *string      `json:"next_cursor"`
	}
	if err := json.NewDecoder(w2.Body).Decode(&page2); err != nil {
		t.Fatalf("page2 decode: %v", err)
	}
	if len(page2.Events) != 2 {
		t.Fatalf("page2 events = %d; want 2", len(page2.Events))
	}
	// Each page-2 event must be strictly older than the oldest page-1 event.
	page1Tail := page1.Events[len(page1.Events)-1].EventID
	for _, e2 := range page2.Events {
		if e2.EventID >= page1Tail {
			t.Errorf("page2 event %q not strictly older than page1 tail %q",
				e2.EventID, page1Tail)
		}
	}
}

func TestAuditListOrderInvalid(t *testing.T) {
	cfg := testConfig(t)
	srv := server.New(cfg)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit?order=sideways", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400 for invalid order", w.Code)
	}
}

// ---- TestAuditShowFound / NotFound ----

func TestAuditShowFound(t *testing.T) {
	cfg := testConfig(t)
	srv := server.New(cfg)
	repo := cfg.RepoRoot

	appendEvent(t, repo, core.Event{
		Actor:  "cam",
		Action: core.ActionCreate,
		Kind:   core.KindDecision,
		ID:     "00000000000000000000000001",
	})

	// Read back to get the real EventID.
	events, err := audit.Read(repo, audit.Filter{})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("no events found")
	}
	eventID := events[len(events)-1].EventID

	req := httptest.NewRequest(http.MethodGet, "/v1/audit/"+eventID, nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", w.Code)
	}
	var ev core.Event
	if err := json.NewDecoder(w.Body).Decode(&ev); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ev.EventID != eventID {
		t.Errorf("event_id = %q; want %q", ev.EventID, eventID)
	}
}

func TestAuditShowNotFound(t *testing.T) {
	cfg := testConfig(t)
	srv := server.New(cfg)

	req := httptest.NewRequest(http.MethodGet, "/v1/audit/NONEXISTENTID00000000000", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d; want 404", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "application/problem+json" {
		t.Errorf("Content-Type = %q; want application/problem+json", ct)
	}
}

// ---- TestAuditExportJSONL ----

func TestAuditExportJSONL(t *testing.T) {
	cfg := testConfig(t)
	srv := server.New(cfg)
	repo := cfg.RepoRoot

	// Append 3 events.
	for i := 0; i < 3; i++ {
		appendEvent(t, repo, core.Event{
			Actor:  "cam",
			Action: core.ActionCreate,
			Kind:   core.KindDecision,
			ID:     fmt.Sprintf("00000000000000000000000%03d", i+1),
		})
		time.Sleep(2 * time.Millisecond)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/audit/export?format=jsonl", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "application/jsonl; charset=utf-8" {
		t.Errorf("Content-Type = %q; want application/jsonl; charset=utf-8", ct)
	}

	// Parse lines and count events.
	body := w.Body.String()
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	count := 0
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var ev core.Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Errorf("invalid JSON line %q: %v", line, err)
			continue
		}
		count++
	}
	if count != 3 {
		t.Errorf("JSONL lines parsed = %d; want 3", count)
	}
}

// ---- TestAuditExportInvalidFormat ----

func TestAuditExportInvalidFormat(t *testing.T) {
	cfg := testConfig(t)
	srv := server.New(cfg)

	req := httptest.NewRequest(http.MethodGet, "/v1/audit/export?format=csv", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "application/problem+json" {
		t.Errorf("Content-Type = %q; want application/problem+json", ct)
	}
}

// ---- TestAuditStreamLiveTail ----

func TestAuditStreamLiveTail(t *testing.T) {
	ts, repo := makeAuditServer(t)

	// Context with timeout so test doesn't hang.
	ctx := t
	_ = ctx

	// Channel to receive the first parsed event from the stream.
	eventCh := make(chan core.Event, 1)
	errCh := make(chan error, 1)

	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		req, err := http.NewRequest(http.MethodGet, ts.URL+"/v1/audit/stream", nil)
		if err != nil {
			errCh <- err
			return
		}

		resp, err := client.Do(req)
		if err != nil {
			errCh <- err
			return
		}
		defer resp.Body.Close()

		sc := bufio.NewScanner(resp.Body)
		var dataLine string
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "data: ") {
				dataLine = strings.TrimPrefix(line, "data: ")
			}
			if line == "" && dataLine != "" {
				// End of SSE event block.
				var ev core.Event
				if err := json.Unmarshal([]byte(dataLine), &ev); err != nil {
					errCh <- fmt.Errorf("SSE data parse error: %w", err)
					return
				}
				eventCh <- ev
				return
			}
		}
		if err := sc.Err(); err != nil {
			errCh <- err
		} else {
			errCh <- fmt.Errorf("stream closed without event")
		}
	}()

	// Give the stream goroutine a moment to connect.
	time.Sleep(100 * time.Millisecond)

	// Append an event that the stream should pick up.
	appendEvent(t, repo, core.Event{
		Actor:  "cam",
		Action: core.ActionCreate,
		Kind:   core.KindDecision,
		ID:     "00000000000000000000000001",
	})

	// Wait up to 1.5s for the event to appear on the stream.
	timeout := time.After(1500 * time.Millisecond)
	select {
	case ev := <-eventCh:
		if ev.Actor != "cam" {
			t.Errorf("SSE event actor = %q; want cam", ev.Actor)
		}
	case err := <-errCh:
		t.Fatalf("SSE stream error: %v", err)
	case <-timeout:
		t.Fatal("timed out waiting for SSE event (1.5s)")
	}
}
