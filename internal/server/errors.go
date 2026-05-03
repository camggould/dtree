// Package server implements the dtree HTTP server.
package server

import (
	"encoding/json"
	"net/http"
)

// Problem is an RFC 9457 Problem Details object.
// Type is a URI string relative to /errors/.
type Problem struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`
}

// WriteProblem writes a Problem Details JSON response.
// It sets Content-Type: application/problem+json, sets the status code,
// marshals the Problem, and sets Instance from r.URL.Path if not already set.
func WriteProblem(w http.ResponseWriter, r *http.Request, p Problem) {
	if p.Instance == "" && r != nil {
		p.Instance = r.URL.Path
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(p)
}

// NotFound returns a 404 Problem Details object.
func NotFound(detail string) Problem {
	return Problem{
		Type:   "/errors/not-found",
		Title:  "Not Found",
		Status: http.StatusNotFound,
		Detail: detail,
	}
}

// BadRequest returns a 400 Problem Details object.
func BadRequest(detail string) Problem {
	return Problem{
		Type:   "/errors/bad-request",
		Title:  "Bad Request",
		Status: http.StatusBadRequest,
		Detail: detail,
	}
}

// Unauthorized returns a 401 Problem Details object.
func Unauthorized(detail string) Problem {
	return Problem{
		Type:   "/errors/unauthorized",
		Title:  "Unauthorized",
		Status: http.StatusUnauthorized,
		Detail: detail,
	}
}

// Forbidden returns a 403 Problem Details object.
func Forbidden(detail string) Problem {
	return Problem{
		Type:   "/errors/forbidden",
		Title:  "Forbidden",
		Status: http.StatusForbidden,
		Detail: detail,
	}
}

// Conflict returns a 409 Problem Details object (for optimistic concurrency).
func Conflict(detail string) Problem {
	return Problem{
		Type:   "/errors/conflict",
		Title:  "Conflict",
		Status: http.StatusConflict,
		Detail: detail,
	}
}

// Unprocessable returns a 422 Problem Details object (for validation errors).
func Unprocessable(detail string) Problem {
	return Problem{
		Type:   "/errors/unprocessable",
		Title:  "Unprocessable Entity",
		Status: http.StatusUnprocessableEntity,
		Detail: detail,
	}
}

// PreconditionFailed returns a 412 Problem Details object (for If-Match
// optimistic-concurrency violations).
func PreconditionFailed(detail string) Problem {
	return Problem{
		Type:   "/errors/precondition-failed",
		Title:  "Precondition Failed",
		Status: http.StatusPreconditionFailed,
		Detail: detail,
	}
}

// Internal returns a 500 Problem Details object.
func Internal(detail string) Problem {
	return Problem{
		Type:   "/errors/internal",
		Title:  "Internal Server Error",
		Status: http.StatusInternalServerError,
		Detail: detail,
	}
}
