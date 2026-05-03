package core

import (
	"encoding/json"
	"testing"
)

// ---------------------------------------------------------------------------
// MarshalJSON / UnmarshalJSON edge cases
// ---------------------------------------------------------------------------

func TestMarshalJSONNilBeforeAfter(t *testing.T) {
	p := EventPayload{} // both Before and After are nil
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal nil payload: %v", err)
	}
	// Should be an empty JSON object, not null.
	if string(data) == "null" {
		t.Error("nil payload marshaled to null, want {}")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if _, ok := raw["before"]; ok {
		t.Error("nil Before should not appear as key in JSON")
	}
	if _, ok := raw["after"]; ok {
		t.Error("nil After should not appear as key in JSON")
	}
}

func TestMarshalJSONOnlyBefore(t *testing.T) {
	p := EventPayload{Before: map[string]any{"status": "proposed"}}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := raw["before"]; !ok {
		t.Error("before key missing")
	}
	if _, ok := raw["after"]; ok {
		t.Error("nil After should not appear as key in JSON")
	}
}

func TestMarshalJSONOnlyAfter(t *testing.T) {
	p := EventPayload{After: map[string]any{"status": "decided"}}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := raw["after"]; !ok {
		t.Error("after key missing")
	}
	if _, ok := raw["before"]; ok {
		t.Error("nil Before should not appear as key in JSON")
	}
}

func TestMarshalJSONExtraKeys(t *testing.T) {
	p := EventPayload{
		Extra: map[string]any{
			"type":   "blocks",
			"target": "01HXKQ5Z3PCWJ8FQR4M2TVB7D9",
		},
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, ok := raw["type"]; !ok {
		t.Error("extra 'type' key missing from flattened JSON")
	}
	if _, ok := raw["target"]; !ok {
		t.Error("extra 'target' key missing from flattened JSON")
	}
}

func TestMarshalJSONStructFieldTakesPrecedenceOverExtra(t *testing.T) {
	// 'before' in Extra should be shadowed by the struct field.
	p := EventPayload{
		Before: map[string]any{"status": "proposed"},
		Extra:  map[string]any{"before": "should-be-shadowed"},
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got EventPayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Before should be the struct-field value, not the extra string.
	if got.Before["status"] != "proposed" {
		t.Errorf("Before.status = %v, want 'proposed'", got.Before["status"])
	}
}

func TestUnmarshalJSONNoKeys(t *testing.T) {
	var p EventPayload
	if err := json.Unmarshal([]byte(`{}`), &p); err != nil {
		t.Fatalf("unmarshal empty: %v", err)
	}
	if p.Before != nil {
		t.Errorf("Before should be nil, got %v", p.Before)
	}
	if p.After != nil {
		t.Errorf("After should be nil, got %v", p.After)
	}
	if p.Extra != nil {
		t.Errorf("Extra should be nil, got %v", p.Extra)
	}
}

func TestUnmarshalJSONOnlyBefore(t *testing.T) {
	var p EventPayload
	if err := json.Unmarshal([]byte(`{"before":{"status":"proposed"}}`), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Before["status"] != "proposed" {
		t.Errorf("Before.status = %v", p.Before["status"])
	}
	if p.After != nil {
		t.Errorf("After should be nil, got %v", p.After)
	}
	if p.Extra != nil {
		t.Errorf("Extra should be nil for no extra keys, got %v", p.Extra)
	}
}

func TestUnmarshalJSONOnlyAfter(t *testing.T) {
	var p EventPayload
	if err := json.Unmarshal([]byte(`{"after":{"status":"decided"}}`), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.After["status"] != "decided" {
		t.Errorf("After.status = %v", p.After["status"])
	}
	if p.Before != nil {
		t.Errorf("Before should be nil, got %v", p.Before)
	}
}

func TestUnmarshalJSONExtraKeys(t *testing.T) {
	var p EventPayload
	const payload = `{"before":{"x":1},"after":{"y":2},"type":"blocks","target":"ULID123"}`
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Before["x"] != float64(1) {
		t.Errorf("Before.x = %v", p.Before["x"])
	}
	if p.After["y"] != float64(2) {
		t.Errorf("After.y = %v", p.After["y"])
	}
	if p.Extra["type"] != "blocks" {
		t.Errorf("Extra.type = %v", p.Extra["type"])
	}
	if p.Extra["target"] != "ULID123" {
		t.Errorf("Extra.target = %v", p.Extra["target"])
	}
}

func TestUnmarshalJSONMalformed(t *testing.T) {
	var p EventPayload
	err := json.Unmarshal([]byte(`not-json`), &p)
	if err == nil {
		t.Error("expected error for malformed JSON, got nil")
	}
}

func TestRoundTripWithAllFields(t *testing.T) {
	original := EventPayload{
		Before: map[string]any{"status": "proposed", "priority": "low"},
		After:  map[string]any{"status": "decided", "priority": "high"},
		Extra: map[string]any{
			"actual_choice":  "Option A",
			"decided_by":     []any{"alice", "bob"},
			"is_recommended": true,
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got EventPayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Before["status"] != "proposed" {
		t.Errorf("Before.status = %v", got.Before["status"])
	}
	if got.After["status"] != "decided" {
		t.Errorf("After.status = %v", got.After["status"])
	}
	if got.Extra["actual_choice"] != "Option A" {
		t.Errorf("Extra.actual_choice = %v", got.Extra["actual_choice"])
	}
}

func TestUnmarshalJSONNullValues(t *testing.T) {
	// before/after present but null.
	var p EventPayload
	if err := json.Unmarshal([]byte(`{"before":null,"after":null}`), &p); err != nil {
		t.Fatalf("unmarshal null values: %v", err)
	}
	// Both should be nil maps.
	if p.Before != nil {
		t.Errorf("Before null should yield nil map, got %v", p.Before)
	}
	if p.After != nil {
		t.Errorf("After null should yield nil map, got %v", p.After)
	}
}
