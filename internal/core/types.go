// Package core defines the domain types for dtree: decisions, trees,
// actors, events, and relationships. These types are the source of truth
// for serialization (YAML on disk, JSON over HTTP/MCP) and for the
// SQLite index. They are deliberately plain data — no behavior here.
package core

import "time"

// SchemaVersion is the on-disk format version for decision YAML files
// and audit event payloads. Bump only on breaking changes; additive
// changes (new optional fields) keep the same version.
const SchemaVersion = 1

// Status is the lifecycle state of a decision.
type Status string

const (
	StatusProposed    Status = "proposed"
	StatusDecided     Status = "decided"
	StatusOutOfScope  Status = "out_of_scope"
	StatusSuperseded  Status = "superseded"
)

// Priority orders decisions for queues and visualization. "assumption"
// is conceptually a kind of decision (a recorded premise), not a true
// urgency level — it sits in the priority enum so all decisions share
// one schema.
type Priority string

const (
	PriorityAssumption Priority = "assumption"
	PriorityLow        Priority = "low"
	PriorityMedium     Priority = "medium"
	PriorityHigh       Priority = "high"
	PriorityCritical   Priority = "critical"
)

// RelationshipType is the locked taxonomy of decision-to-decision edges.
type RelationshipType string

const (
	RelBlocks      RelationshipType = "blocks"
	RelInfluences  RelationshipType = "influences"
	RelSupersedes  RelationshipType = "supersedes"
	RelRelatesTo   RelationshipType = "relates_to"
)

// ActorKind distinguishes humans from automated agents. Used for governance
// metrics (e.g. agent recommendation acceptance rate).
type ActorKind string

const (
	ActorHuman ActorKind = "human"
	ActorAgent ActorKind = "agent"
)

// Action is the discriminator on audit events.
type Action string

const (
	ActionCreate          Action = "create"
	ActionUpdate          Action = "update"
	ActionDelete          Action = "delete"
	ActionDecide          Action = "decide"
	ActionUndecide        Action = "undecide"
	ActionScopeOut        Action = "scope_out"
	ActionSupersede       Action = "supersede"
	ActionRelate          Action = "relate"
	ActionUnrelate        Action = "unrelate"
	ActionRename          Action = "rename"
	ActionRestore         Action = "restore"
	ActionExternalEdit    Action = "external_edit"
	ActionExternalCreate  Action = "external_create"
	ActionExternalDelete  Action = "external_delete"
	ActionTreeCreate      Action = "tree_create"
	ActionTreeDelete      Action = "tree_delete"
	ActionTreeRename      Action = "tree_rename"
	ActionTreeArchive     Action = "tree_archive"
	ActionActorAdd        Action = "actor_add"
	ActionActorRename     Action = "actor_rename"
	ActionActorArchive    Action = "actor_archive"
	ActionConfigChange    Action = "config_change"
	ActionSchemaMigrate   Action = "schema_migrate"
)

// Kind discriminates which kind of entity an event acts on.
type Kind string

const (
	KindDecision     Kind = "decision"
	KindTree         Kind = "tree"
	KindActor        Kind = "actor"
	KindRelationship Kind = "relationship"
	KindConfig       Kind = "config"
)

// Relationship is one directed edge between decisions. Stored inline
// in the source decision's YAML; cross-tree edges use the bare ULID
// since IDs are globally unique.
type Relationship struct {
	Type   RelationshipType `json:"type" yaml:"type" validate:"required,oneof=blocks influences supersedes relates_to"`
	Target string           `json:"target" yaml:"target" validate:"required,len=26"`
}

// Decision is the canonical on-disk shape (modulo the YAML body fields,
// which are stored as block scalars). The same struct serves as the
// HTTP/MCP API response and the SQLite-row materialization.
type Decision struct {
	ID            string   `json:"id" yaml:"id" validate:"required,len=26"`
	Slug          string   `json:"slug" yaml:"slug" validate:"required"`
	SchemaVersion int      `json:"schema_version" yaml:"schema_version"`
	Tree          string   `json:"tree" yaml:"-"` // tree slug; derived from path on read
	Summary       string   `json:"summary" yaml:"summary" validate:"required,min=1,max=200"`
	Priority      Priority `json:"priority" yaml:"priority" validate:"required,oneof=assumption low medium high critical"`
	Status        Status   `json:"status" yaml:"status" validate:"required,oneof=proposed decided out_of_scope superseded"`

	Creator  string   `json:"creator" yaml:"creator" validate:"required"`
	Assignee string   `json:"assignee,omitempty" yaml:"assignee,omitempty"`
	Tags     []string `json:"tags,omitempty" yaml:"tags,omitempty"`

	// Recommendation. recommended_by is single (one author) and may be
	// distinct from creator; populated when a recommendation is set/updated.
	RecommendedSummary string `json:"recommended_summary,omitempty" yaml:"recommended_summary,omitempty"`
	RecommendedFull    string `json:"recommended_full,omitempty" yaml:"recommended_full,omitempty"`
	RecommendedBy      string `json:"recommended_by,omitempty" yaml:"recommended_by,omitempty"`

	// Outcome. Populated when status=decided. decided_by is a list to
	// support committee decisions.
	ActualChoice       string   `json:"actual_choice,omitempty" yaml:"actual_choice,omitempty"`
	ActualChoiceReason string   `json:"actual_choice_reason,omitempty" yaml:"actual_choice_reason,omitempty"`
	DecidedBy          []string `json:"decided_by,omitempty" yaml:"decided_by,omitempty"`
	IsRecommended      bool     `json:"is_recommended" yaml:"is_recommended"`

	// Out-of-scope reason. Populated when status=out_of_scope.
	OutOfScopeReason string `json:"out_of_scope_reason,omitempty" yaml:"out_of_scope_reason,omitempty"`

	// Long-form prose; goes in YAML body as a block scalar.
	Description string `json:"decision_full_description,omitempty" yaml:"decision_full_description,omitempty"`

	// Outgoing relationships. Inbound edges are derived via the index.
	Relationships []Relationship `json:"relationships,omitempty" yaml:"relationships,omitempty"`

	// Rev is the optimistic-concurrency token: the event_id of the most
	// recent event touching this decision. Not stored on disk; populated
	// by the index for HTTP If-Match and CLI --expect-rev checks.
	Rev string `json:"_rev,omitempty" yaml:"-"`
}

// Tree metadata. Stored at .decisions/<slug>/tree.yaml.
type Tree struct {
	Slug          string    `json:"slug" yaml:"slug" validate:"required"`
	SchemaVersion int       `json:"schema_version" yaml:"schema_version"`
	Title         string    `json:"title,omitempty" yaml:"title,omitempty"`
	Description   string    `json:"description,omitempty" yaml:"description,omitempty"`
	Archived      bool      `json:"archived" yaml:"archived"`
	CreatedAt     time.Time `json:"created_at" yaml:"created_at"`
	Layout        struct {
		Direction string `json:"direction,omitempty" yaml:"direction,omitempty"` // TB | LR
	} `json:"layout,omitempty" yaml:"layout,omitempty"`
}

// Actor is one registered identity (human or agent). Stored as an entry
// in the project-level actors.yaml.
type Actor struct {
	Handle string    `json:"handle" yaml:"handle" validate:"required,min=1,max=64"`
	Name   string    `json:"name,omitempty" yaml:"name,omitempty"`
	Email  string    `json:"email,omitempty" yaml:"email,omitempty"`
	Kind   ActorKind `json:"kind" yaml:"kind" validate:"required,oneof=human agent"`
	Active bool      `json:"active" yaml:"active"`
}

// Event is one entry in the audit log. The payload is action-specific
// and serialized to/from JSON inside the JSONL line.
type Event struct {
	EventID string          `json:"event_id" validate:"required,len=26"`
	V       int             `json:"v"`
	Ts      time.Time       `json:"ts" validate:"required"`
	Actor   string          `json:"actor" validate:"required"`
	Action  Action          `json:"action" validate:"required"`
	Kind    Kind            `json:"kind" validate:"required"`
	Tree    string          `json:"tree,omitempty"` // omitted for repo-level events
	ID      string          `json:"id" validate:"required"`
	Payload EventPayload    `json:"payload"`
}

// EventPayload carries action-specific data. Most actions populate
// Before/After diffs; the broader map captures anything not modeled.
type EventPayload struct {
	Before map[string]any `json:"before,omitempty"`
	After  map[string]any `json:"after,omitempty"`
	// Extra is preserved verbatim for action-specific keys (e.g.
	// relate's type/target, decide's summary fields).
	Extra map[string]any `json:"-"`
}
