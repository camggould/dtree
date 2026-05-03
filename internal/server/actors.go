package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/identity"
	"github.com/cgould/dtree/internal/ulid"
)

// ---------------------------------------------------------------------------
// Mount
// ---------------------------------------------------------------------------

// mountActors registers all /v1/actors routes.
func mountActors(r chi.Router, cfg Config) {
	r.Route("/actors", func(r chi.Router) {
		r.Get("/", listActorsHandler(cfg))
		r.Post("/", createActorHandler(cfg))
		r.Route("/{handle}", func(r chi.Router) {
			r.Get("/", getActorHandler(cfg))
			r.Patch("/", patchActorHandler(cfg))
			r.Post("/archive", archiveActorHandler(cfg))
			r.Post("/rename", renameActorHandler(cfg))
		})
	})
}

// ---------------------------------------------------------------------------
// Response shapes
// ---------------------------------------------------------------------------

type actorListResponse struct {
	Items []core.Actor `json:"items"`
}

// ---------------------------------------------------------------------------
// GET /v1/actors
// ---------------------------------------------------------------------------

func listActorsHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		includeArchived := r.URL.Query().Get("include_archived") == "true"

		af, err := cfg.Resolver.LoadActors()
		if err != nil {
			WriteProblem(w, r, Internal("failed to load actors"))
			return
		}

		actors := af.Actors
		if !includeArchived {
			var filtered []core.Actor
			for _, a := range actors {
				if a.Active {
					filtered = append(filtered, a)
				}
			}
			actors = filtered
		}
		if actors == nil {
			actors = []core.Actor{}
		}

		writeJSON(w, http.StatusOK, actorListResponse{Items: actors})
	}
}

// ---------------------------------------------------------------------------
// GET /v1/actors/{handle}
// ---------------------------------------------------------------------------

func getActorHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handle := chi.URLParam(r, "handle")
		actor, err := cfg.Resolver.FindActor(handle)
		if err != nil {
			WriteProblem(w, r, Internal("failed to look up actor"))
			return
		}
		if actor == nil {
			WriteProblem(w, r, NotFound("actor not found: "+handle))
			return
		}
		writeJSON(w, http.StatusOK, actor)
	}
}

// ---------------------------------------------------------------------------
// POST /v1/actors
// ---------------------------------------------------------------------------

type createActorRequest struct {
	Handle      string         `json:"handle"`
	Kind        core.ActorKind `json:"kind"`
	DisplayName string         `json:"display_name"`
	Email       string         `json:"email"`
}

func createActorHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if readOnlyGuard(w, r, cfg) {
			return
		}
		if !MustHaveIdentity(w, r) {
			return
		}
		actor, _ := IdentityFromContext(r.Context())

		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			WriteProblem(w, r, BadRequest("failed to read body"))
			return
		}
		var req createActorRequest
		if err := json.Unmarshal(body, &req); err != nil {
			WriteProblem(w, r, BadRequest("invalid JSON: "+err.Error()))
			return
		}
		if req.Handle == "" {
			WriteProblem(w, r, BadRequest("handle is required"))
			return
		}
		if req.Kind == "" {
			req.Kind = core.ActorHuman
		}

		newActor := core.Actor{
			Handle: req.Handle,
			Name:   req.DisplayName,
			Email:  req.Email,
			Kind:   req.Kind,
			Active: true,
		}

		if err := cfg.Resolver.AddActor(newActor); err != nil {
			if errors.Is(err, identity.ErrActorExists) {
				WriteProblem(w, r, Conflict("actor already exists: "+req.Handle))
				return
			}
			if errors.Is(err, identity.ErrInvalidHandle) {
				WriteProblem(w, r, BadRequest("invalid handle: "+req.Handle))
				return
			}
			WriteProblem(w, r, Internal("failed to add actor: "+err.Error()))
			return
		}

		_ = audit.Append(cfg.RepoRoot, core.Event{
			EventID: ulid.New(),
			V:       1,
			Ts:      time.Now().UTC(),
			Actor:   actor,
			Action:  core.ActionActorAdd,
			Kind:    core.KindActor,
			ID:      req.Handle,
			Payload: core.EventPayload{
				After: map[string]any{
					"handle": req.Handle,
					"kind":   req.Kind,
					"name":   req.DisplayName,
					"email":  req.Email,
				},
			},
		})

		found, err := cfg.Resolver.FindActor(req.Handle)
		if err != nil || found == nil {
			WriteProblem(w, r, Internal("failed to read back actor"))
			return
		}
		writeJSON(w, http.StatusCreated, found)
	}
}

// ---------------------------------------------------------------------------
// PATCH /v1/actors/{handle}
// ---------------------------------------------------------------------------

type patchActorRequest struct {
	DisplayName *string         `json:"display_name"`
	Email       *string         `json:"email"`
	Kind        *core.ActorKind `json:"kind"`
}

func patchActorHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if readOnlyGuard(w, r, cfg) {
			return
		}
		if !MustHaveIdentity(w, r) {
			return
		}
		actor, _ := IdentityFromContext(r.Context())
		handle := chi.URLParam(r, "handle")

		existing, err := cfg.Resolver.FindActor(handle)
		if err != nil {
			WriteProblem(w, r, Internal("failed to look up actor"))
			return
		}
		if existing == nil {
			WriteProblem(w, r, NotFound("actor not found: "+handle))
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			WriteProblem(w, r, BadRequest("failed to read body"))
			return
		}
		var req patchActorRequest
		if err := json.Unmarshal(body, &req); err != nil {
			WriteProblem(w, r, BadRequest("invalid JSON: "+err.Error()))
			return
		}

		before := *existing
		if err := cfg.Resolver.UpdateActor(handle, func(a *core.Actor) {
			if req.DisplayName != nil {
				a.Name = *req.DisplayName
			}
			if req.Email != nil {
				a.Email = *req.Email
			}
			if req.Kind != nil {
				a.Kind = *req.Kind
			}
		}); err != nil {
			if errors.Is(err, identity.ErrActorNotFound) {
				WriteProblem(w, r, NotFound("actor not found: "+handle))
				return
			}
			WriteProblem(w, r, Internal("failed to update actor: "+err.Error()))
			return
		}

		updated, err := cfg.Resolver.FindActor(handle)
		if err != nil || updated == nil {
			WriteProblem(w, r, Internal("failed to read back actor"))
			return
		}

		_ = audit.Append(cfg.RepoRoot, core.Event{
			EventID: ulid.New(),
			V:       1,
			Ts:      time.Now().UTC(),
			Actor:   actor,
			Action:  core.ActionUpdate,
			Kind:    core.KindActor,
			ID:      handle,
			Payload: core.EventPayload{
				Before: actorToMap(before),
				After:  actorToMap(*updated),
			},
		})

		writeJSON(w, http.StatusOK, updated)
	}
}

// ---------------------------------------------------------------------------
// POST /v1/actors/{handle}/archive
// ---------------------------------------------------------------------------

func archiveActorHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if readOnlyGuard(w, r, cfg) {
			return
		}
		if !MustHaveIdentity(w, r) {
			return
		}
		actor, _ := IdentityFromContext(r.Context())
		handle := chi.URLParam(r, "handle")

		existing, err := cfg.Resolver.FindActor(handle)
		if err != nil {
			WriteProblem(w, r, Internal("failed to look up actor"))
			return
		}
		if existing == nil {
			WriteProblem(w, r, NotFound("actor not found: "+handle))
			return
		}

		if err := cfg.Resolver.ArchiveActor(handle); err != nil {
			if errors.Is(err, identity.ErrActorNotFound) {
				WriteProblem(w, r, NotFound("actor not found: "+handle))
				return
			}
			if errors.Is(err, identity.ErrActorAlreadyArchived) {
				WriteProblem(w, r, Conflict("actor already archived: "+handle))
				return
			}
			WriteProblem(w, r, Internal("failed to archive actor: "+err.Error()))
			return
		}

		_ = audit.Append(cfg.RepoRoot, core.Event{
			EventID: ulid.New(),
			V:       1,
			Ts:      time.Now().UTC(),
			Actor:   actor,
			Action:  core.ActionActorArchive,
			Kind:    core.KindActor,
			ID:      handle,
		})

		w.WriteHeader(http.StatusNoContent)
	}
}

// ---------------------------------------------------------------------------
// POST /v1/actors/{handle}/rename
// ---------------------------------------------------------------------------

type renameActorRequest struct {
	NewHandle string `json:"new_handle"`
}

func renameActorHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if readOnlyGuard(w, r, cfg) {
			return
		}
		if !MustHaveIdentity(w, r) {
			return
		}
		actor, _ := IdentityFromContext(r.Context())
		handle := chi.URLParam(r, "handle")

		existing, err := cfg.Resolver.FindActor(handle)
		if err != nil {
			WriteProblem(w, r, Internal("failed to look up actor"))
			return
		}
		if existing == nil {
			WriteProblem(w, r, NotFound("actor not found: "+handle))
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			WriteProblem(w, r, BadRequest("failed to read body"))
			return
		}
		var req renameActorRequest
		if err := json.Unmarshal(body, &req); err != nil {
			WriteProblem(w, r, BadRequest("invalid JSON: "+err.Error()))
			return
		}
		if req.NewHandle == "" {
			WriteProblem(w, r, BadRequest("new_handle is required"))
			return
		}

		if err := cfg.Resolver.RenameActor(handle, req.NewHandle); err != nil {
			if errors.Is(err, identity.ErrActorNotFound) {
				WriteProblem(w, r, NotFound("actor not found: "+handle))
				return
			}
			if errors.Is(err, identity.ErrInvalidHandle) {
				WriteProblem(w, r, BadRequest("invalid new_handle: "+req.NewHandle))
				return
			}
			if errors.Is(err, identity.ErrActorExists) {
				WriteProblem(w, r, Conflict("actor already exists: "+req.NewHandle))
				return
			}
			WriteProblem(w, r, Internal("failed to rename actor: "+err.Error()))
			return
		}

		_ = audit.Append(cfg.RepoRoot, core.Event{
			EventID: ulid.New(),
			V:       1,
			Ts:      time.Now().UTC(),
			Actor:   actor,
			Action:  core.ActionActorRename,
			Kind:    core.KindActor,
			ID:      handle,
			Payload: core.EventPayload{
				Extra: map[string]any{"new_handle": req.NewHandle},
			},
		})

		found, err := cfg.Resolver.FindActor(req.NewHandle)
		if err != nil || found == nil {
			WriteProblem(w, r, Internal("failed to read back renamed actor"))
			return
		}
		writeJSON(w, http.StatusOK, found)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func actorToMap(a core.Actor) map[string]any {
	return map[string]any{
		"handle": a.Handle,
		"name":   a.Name,
		"email":  a.Email,
		"kind":   string(a.Kind),
		"active": a.Active,
	}
}
