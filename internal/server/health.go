package server

import (
	"encoding/json"
	"net/http"

	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/index"
)

// healthResponse is the JSON body returned by GET /v1/health.
type healthResponse struct {
	OK      bool           `json:"ok"`
	Version string         `json:"version"`
	Schema  healthSchema   `json:"schema"`
}

type healthSchema struct {
	Core  int `json:"core"`
	Index int `json:"index"`
}

// healthHandler returns a handler for GET /v1/health.
// No identity is required for this endpoint.
func healthHandler(version string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := healthResponse{
			OK:      true,
			Version: version,
			Schema: healthSchema{
				Core:  core.SchemaVersion,
				Index: index.CurrentSchemaVersion,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(resp)
	}
}
