// Package uifs embeds the compiled UI assets and exposes an http.Handler
// that serves them, with SPA fallback to index.html for non-API routes.
package uifs

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// FS returns the embedded filesystem rooted at the dist/ directory.
func FS() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// This should never fail since dist/ is always embedded.
		panic("uifs: failed to sub dist: " + err.Error())
	}
	return sub
}

// Handler returns an http.Handler that serves the embedded UI assets.
// Requests that look like API calls (path starts with /v1) are passed
// through (returned as 404 from this handler so the caller's router can
// handle them). All other paths not matching a real file fall back to
// index.html for client-side routing.
func Handler() http.Handler {
	uiFS := FS()
	fileServer := http.FileServer(http.FS(uiFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Never serve /api/ or /v1/ paths from the UI embed.
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/v1/") {
			http.NotFound(w, r)
			return
		}

		// Strip /ui/ prefix before looking up the file.
		path := strings.TrimPrefix(r.URL.Path, "/ui")
		if path == "" {
			path = "/"
		}

		// Try to open the file. If it doesn't exist, serve index.html.
		if path != "/" && path != "" {
			// Remove leading slash for fs.Open
			cleaned := strings.TrimPrefix(path, "/")
			if cleaned == "" {
				cleaned = "."
			}
			f, err := uiFS.Open(cleaned)
			if err != nil {
				// File not found — serve index.html for SPA routing
				serveIndexHTML(w, r, uiFS)
				return
			}
			_ = f.Close()
		}

		// Rewrite the request path so FileServer sees the correct path
		r2 := r.Clone(r.Context())
		r2.URL.Path = path
		fileServer.ServeHTTP(w, r2)
	})
}

// serveIndexHTML reads index.html from the embedded FS and writes it to w.
func serveIndexHTML(w http.ResponseWriter, r *http.Request, uiFS fs.FS) {
	data, err := fs.ReadFile(uiFS, "index.html")
	if err != nil {
		// index.html not present (e.g. dist/ is empty during dev)
		http.Error(w, "UI not built. Run: make ui", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}
