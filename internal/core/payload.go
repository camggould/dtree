package core

import "encoding/json"

// MarshalJSON emits EventPayload as a flat JSON object with before/after at
// the top level and all Extra entries merged in alongside them.
//
// {"before": {...}, "after": {...}, "extra-key": value, ...}
//
// Keys from Extra that collide with "before" or "after" are silently shadowed
// by the struct fields.
func (p EventPayload) MarshalJSON() ([]byte, error) {
	// Start with the known fields.
	m := make(map[string]any, 2+len(p.Extra))
	if p.Before != nil {
		m["before"] = p.Before
	}
	if p.After != nil {
		m["after"] = p.After
	}
	// Merge extra keys. Struct fields take precedence; we wrote them first
	// so we only add keys that don't already exist.
	for k, v := range p.Extra {
		if _, exists := m[k]; !exists {
			m[k] = v
		}
	}
	return json.Marshal(m)
}

// UnmarshalJSON parses a flat payload object. "before" and "after" are
// extracted into the struct fields; every other key is collected into Extra.
func (p *EventPayload) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	if b, ok := raw["before"]; ok {
		if err := json.Unmarshal(b, &p.Before); err != nil {
			return err
		}
		delete(raw, "before")
	}
	if a, ok := raw["after"]; ok {
		if err := json.Unmarshal(a, &p.After); err != nil {
			return err
		}
		delete(raw, "after")
	}

	// Remaining keys → Extra.
	if len(raw) > 0 {
		p.Extra = make(map[string]any, len(raw))
		for k, v := range raw {
			var val any
			if err := json.Unmarshal(v, &val); err != nil {
				return err
			}
			p.Extra[k] = val
		}
	}
	return nil
}
