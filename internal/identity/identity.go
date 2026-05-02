// Package identity is the source of truth for "who is acting right now"
// and "is that actor registered in this project". It bridges the config
// layer (three-layer identity resolution) with the project's actors.yaml
// registry, and provides the mutation helpers that the actor-management
// commands delegate to.
package identity

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"unicode"

	"github.com/cgould/dtree/internal/config"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/storage"
)

// Source mirrors config.Source — re-exported for clarity at this layer.
type Source = config.Source

// Sentinel errors returned by Resolver methods.
var (
	// ErrActorNotFound is returned when a handle is not in actors.yaml.
	ErrActorNotFound = errors.New("identity: actor not found")

	// ErrActorExists is returned when an actor with the same handle
	// already exists but has different fields.
	ErrActorExists = errors.New("identity: actor already exists")

	// ErrActorAlreadyArchived is returned by ArchiveActor when the
	// actor is already inactive.
	ErrActorAlreadyArchived = errors.New("identity: actor already archived")

	// ErrInvalidHandle is returned when a handle fails format validation.
	ErrInvalidHandle = errors.New("identity: invalid handle")
)

// handleRE is the allowed pattern for actor handles.
var handleRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.\-]{0,63}$`)

// Resolution is the result of a single identity resolution call.
type Resolution struct {
	Handle    string      // "" iff unresolved
	Source    Source      // SourceFlag/Env/Local/Global/Default
	Actor     *core.Actor // populated iff registered in this project
	InProject bool        // false iff Handle resolves but not in actors.yaml
}

// Resolver coordinates identity lookups against config and project actors.
type Resolver struct {
	repoRoot string
	cfg      *config.Resolved
	actors   *storage.ActorsFile // loaded lazily; nil before first LoadActors
}

// NewResolver constructs a Resolver for the given repository root and
// resolved configuration.
func NewResolver(repoRoot string, cfg *config.Resolved) *Resolver {
	return &Resolver{repoRoot: repoRoot, cfg: cfg}
}

// actorsPath returns the absolute path of actors.yaml for this project.
func (r *Resolver) actorsPath() string {
	return filepath.Join(r.repoRoot, ".decisions", storage.ActorsFileName)
}

// Resolve returns the active identity. flagOverride takes precedence over
// everything (used by --as flag); pass "" if no flag was given.
//
// If the resolved handle is not registered in the project's actors.yaml,
// Resolution.InProject is false. Callers may choose to error or auto-register.
func (r *Resolver) Resolve(flagOverride string) (*Resolution, error) {
	var handle string
	var src Source

	if flagOverride != "" {
		handle = flagOverride
		src = config.SourceFlag
	} else {
		handle = r.cfg.Identity
		src = r.cfg.IdentitySrc
	}

	res := &Resolution{
		Handle: handle,
		Source: src,
	}

	if handle == "" {
		return res, nil
	}

	actor, err := r.FindActor(handle)
	if err != nil {
		return nil, err
	}
	if actor != nil {
		res.Actor = actor
		res.InProject = true
	}

	return res, nil
}

// MustResolve is like Resolve but returns an actionable error when identity
// is absent or not registered in the project.
//
// Errors look like:
//
//	"no identity configured; run `dtree config set --global identity.default <handle>`"
//	"identity \"alice\" is not registered in this project; run `dtree actor add alice`"
func (r *Resolver) MustResolve(flagOverride string) (*Resolution, error) {
	res, err := r.Resolve(flagOverride)
	if err != nil {
		return nil, err
	}
	if res.Handle == "" {
		return nil, errors.New(
			"no identity configured; run `dtree config set --global identity.default <handle>`",
		)
	}
	if !res.InProject {
		return nil, fmt.Errorf(
			"identity %q is not registered in this project; run `dtree actor add %s`",
			res.Handle, res.Handle,
		)
	}
	return res, nil
}

// LoadActors reads actors.yaml and caches the result on the Resolver.
// Returns an empty ActorsFile if the file is missing — that is not an error.
func (r *Resolver) LoadActors() (*storage.ActorsFile, error) {
	if r.actors != nil {
		return r.actors, nil
	}
	af, err := storage.ReadActors(r.actorsPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			af = &storage.ActorsFile{}
		} else {
			return nil, err
		}
	}
	r.actors = af
	return r.actors, nil
}

// FindActor returns the actor record for handle, or nil if not registered.
func (r *Resolver) FindActor(handle string) (*core.Actor, error) {
	af, err := r.LoadActors()
	if err != nil {
		return nil, err
	}
	for i := range af.Actors {
		if af.Actors[i].Handle == handle {
			a := af.Actors[i]
			return &a, nil
		}
	}
	return nil, nil
}

// AddActor registers a new actor in actors.yaml. Idempotent: if handle
// already exists with identical fields, returns nil. If the handle exists
// with different fields, returns ErrActorExists.
func (r *Resolver) AddActor(a core.Actor) error {
	if err := validateHandle(a.Handle); err != nil {
		return err
	}
	if err := validateKind(a.Kind); err != nil {
		return err
	}

	af, err := r.LoadActors()
	if err != nil {
		return err
	}

	for _, existing := range af.Actors {
		if existing.Handle == a.Handle {
			if existing == a {
				return nil // idempotent no-op
			}
			return fmt.Errorf("%w: %q", ErrActorExists, a.Handle)
		}
	}

	af.Actors = append(af.Actors, a)
	if err := r.writeActors(af); err != nil {
		return err
	}
	return nil
}

// UpdateActor mutates name/email/kind/active for an existing handle.
// Handle is immutable — returns ErrActorNotFound if not present.
func (r *Resolver) UpdateActor(handle string, mutate func(*core.Actor)) error {
	af, err := r.LoadActors()
	if err != nil {
		return err
	}

	idx := r.indexOf(af, handle)
	if idx < 0 {
		return fmt.Errorf("%w: %q", ErrActorNotFound, handle)
	}

	mutate(&af.Actors[idx])
	return r.writeActors(af)
}

// RenameActor changes a handle in actors.yaml. The caller is responsible for
// rewriting references across decisions — this package only touches actors.yaml.
// Returns ErrActorNotFound if oldHandle is missing, ErrActorExists if
// newHandle is already taken.
func (r *Resolver) RenameActor(oldHandle, newHandle string) error {
	if err := validateHandle(newHandle); err != nil {
		return err
	}

	af, err := r.LoadActors()
	if err != nil {
		return err
	}

	oldIdx := r.indexOf(af, oldHandle)
	if oldIdx < 0 {
		return fmt.Errorf("%w: %q", ErrActorNotFound, oldHandle)
	}
	if r.indexOf(af, newHandle) >= 0 {
		return fmt.Errorf("%w: %q", ErrActorExists, newHandle)
	}

	af.Actors[oldIdx].Handle = newHandle
	return r.writeActors(af)
}

// ArchiveActor sets active=false for handle. The record is preserved for
// historical references. Returns ErrActorAlreadyArchived if already inactive.
func (r *Resolver) ArchiveActor(handle string) error {
	af, err := r.LoadActors()
	if err != nil {
		return err
	}

	idx := r.indexOf(af, handle)
	if idx < 0 {
		return fmt.Errorf("%w: %q", ErrActorNotFound, handle)
	}
	if !af.Actors[idx].Active {
		return fmt.Errorf("%w: %q", ErrActorAlreadyArchived, handle)
	}

	af.Actors[idx].Active = false
	return r.writeActors(af)
}

// writeActors persists af to disk and updates the cached copy.
func (r *Resolver) writeActors(af *storage.ActorsFile) error {
	if err := os.MkdirAll(filepath.Dir(r.actorsPath()), 0o755); err != nil {
		return fmt.Errorf("identity: mkdir %s: %w", filepath.Dir(r.actorsPath()), err)
	}
	if err := storage.WriteActors(r.actorsPath(), af); err != nil {
		return err
	}
	r.actors = af
	return nil
}

// indexOf returns the index of handle in af.Actors, or -1.
func (r *Resolver) indexOf(af *storage.ActorsFile, handle string) int {
	for i := range af.Actors {
		if af.Actors[i].Handle == handle {
			return i
		}
	}
	return -1
}

// validateHandle returns ErrInvalidHandle if handle violates format rules.
func validateHandle(handle string) error {
	if handle == "" {
		return fmt.Errorf("%w: handle must not be empty", ErrInvalidHandle)
	}
	for _, r := range handle {
		if unicode.IsSpace(r) {
			return fmt.Errorf("%w: handle must not contain whitespace", ErrInvalidHandle)
		}
	}
	if len(handle) > 64 {
		return fmt.Errorf("%w: handle exceeds 64 characters", ErrInvalidHandle)
	}
	if !handleRE.MatchString(handle) {
		return fmt.Errorf(
			"%w: handle %q must match ^[A-Za-z][A-Za-z0-9_.-]{0,63}$",
			ErrInvalidHandle, handle,
		)
	}
	return nil
}

// validateKind returns an error if kind is not ActorHuman or ActorAgent.
func validateKind(kind core.ActorKind) error {
	switch kind {
	case core.ActorHuman, core.ActorAgent:
		return nil
	default:
		return fmt.Errorf("identity: invalid actor kind %q; must be %q or %q",
			kind, core.ActorHuman, core.ActorAgent)
	}
}
