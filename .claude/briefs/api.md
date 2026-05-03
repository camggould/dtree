=== DTREE API BRIEFING (do not re-read these files) ===

MODULE: github.com/cgould/dtree   GO 1.25.5   BUILD TAG: -tags sqlite_fts5 (REQUIRED)

WORKTREE NOTE: branch starts bare. First action: `git merge main --no-edit` then `cd $(pwd)`.

--- internal/core/types.go ---
const SchemaVersion = 1
type Status string         // StatusProposed, StatusDecided, StatusOutOfScope, StatusSuperseded
type Priority string       // PriorityAssumption, PriorityLow, PriorityMedium, PriorityHigh, PriorityCritical
type RelationshipType string // RelBlocks, RelInfluences, RelSupersedes, RelRelatesTo
type ActorKind string      // "human" | "agent"
type Action string         // ActionCreate, Update, Delete, Decide, Undecide, ScopeOut, Supersede,
                           //   Relate, Unrelate, Rename, Restore, ExternalEdit/Create/Delete,
                           //   TreeCreate/Delete/Rename/Archive, ActorAdd/Rename/Archive,
                           //   ConfigChange, SchemaMigrate
type Kind string           // KindDecision, KindTree, KindActor, KindRelationship, KindConfig

type Relationship struct { Type RelationshipType; Target string; Note string }

type Decision struct {
  ID, Tree, Slug, Summary, Description string
  Priority Priority; Status Status
  Tags []string; Creator, Assignee string
  RecommendedSummary, RecommendedFull, RecommendedBy string
  IsRecommended bool
  ActualChoice, ActualChoiceReason string
  DecidedBy []string; DecidedAt *time.Time
  CreatedAt, UpdatedAt time.Time
  Relationships []Relationship
  Rev string  // optimistic concurrency token
}

type Tree struct { Slug, Name, Description string; Archived bool; CreatedAt, UpdatedAt time.Time }
type Actor struct { Handle, DisplayName string; Kind ActorKind; Email string; Archived bool }
type Event struct {
  ID string; Ts time.Time
  Actor string; Action Action; Kind Kind
  Tree, ID2 string  // ID2 == target id
  Payload EventPayload
}
type EventPayload struct { Before, After map[string]any; Extra map[string]any }
// EventPayload has custom MarshalJSON that flattens Extra alongside before/after.

--- internal/index ---
func Open(path string) (*DB, error)                  // sets WAL, busy_timeout, FK, runs CreateSchema
func (db *DB) Conn() *sql.DB
func (db *DB) Close() error
func CurrentSchemaVersion = N (read schema.go for value)
func GetDecision(db *DB, id string) (*core.Decision, error)
func GetDecisionRev(db *DB, id string) (string, error)
func InsertDecision(db *DB, d *core.Decision, contentSha string) error
func UpdateDecision(db *DB, d *core.Decision, contentSha, newRev string) error
func UpdateDecisionWithExpectedRev(db, d, sha, expectedRev, newRev string) error  // returns *concurrency.Conflict
func DeleteDecision(db *DB, id string) error
func DeleteDecisionWithExpectedRev(db, id, expectedRev string) error
// schema columns: decisions(id, tree, slug, summary, description, priority, status,
//   creator, assignee, recommended_summary, recommended_full, recommended_by,
//   is_recommended, actual_choice, actual_choice_reason, decided_at, deleted, rev)
//   side: decision_deciders(decision_id, handle), decision_tags(decision_id, tag),
//   relationships(source, target, type, note, tree)
//   events(...), trees(slug, name, description, archived, created_at, updated_at)
//   actors via actors.yaml (NOT in DB)
//   FTS5: decisions_fts(content='decisions', content_rowid='rowid'); join on rowid

--- internal/storage/yaml.go ---
func ReadDecision(path) (*Decision, error)
func WriteDecision(path, *Decision) error    // atomic tmp+fsync+rename
func DecisionPath(treeDir, id, slug) string  // = treeDir/decisions/<id>-<slug>.yaml
func ReadActors(path) (*ActorsFile, error)   // ActorsFile{Actors []Actor}
func WriteActors(path, *ActorsFile) error
func ReadTree(path), WriteTree, ReadTrees, WriteTrees
func MarshalDecisionJSON(*Decision) ([]byte, error)
func SlugFromSummary(summary string) string

--- internal/audit/audit.go ---
func Append(repoRoot string, ev core.Event) error  // monthly JSONL .decisions/<tree>/audit/YYYY-MM.jsonl
func Read(repoRoot string, f Filter) ([]core.Event, error)  // Filter{Tree, Ref, Since, Until, Action}
func ReplayState(repoRoot, tree string, at time.Time) (map[string]*core.Decision, error)

--- internal/concurrency ---
func NewRev() string  // ULID
func AsConflict(err) (*Conflict, bool)  // Conflict{DecisionID, ExpectedRev, ActualRev}
var ErrConflict = errors.New(...)

--- internal/identity/identity.go ---
func NewResolver(repoRoot, *config.Resolved) *Resolver
func (r) Resolve(flagOverride string) (*Resolution, error)  // Resolution{Handle, Actor}
func (r) MustResolve(flag) (*Resolution, error)
func (r) FindActor(handle) (*Actor, error)
func (r) AddActor(Actor) error
func (r) UpdateActor(handle, mutate func(*Actor)) error
func (r) RenameActor(old, new string) error
func (r) ArchiveActor(handle) error
func (r) LoadActors() (*ActorsFile, error)

--- internal/config ---
func Load(repoRoot) (*Resolved, error)  // Resolved{Identity, Tree, Editor, Output, Color}

--- internal/validate ---
func Decision(*Decision) error  // go-playground/validator-style required+enum checks

--- internal/fsutil ---
func Sha256File(path) (string, error)

--- internal/ulid ---
func New() string  // 26-char Crockford uppercase, monotonic

--- internal/server (existing helpers) ---
type Config struct { Listen, RepoRoot string; DB *index.DB; Resolver *identity.Resolver; ReadOnly bool; Trust Trust; Version string }
func New(cfg) *http.Server
func WriteProblem(w, r, *Problem)
func NotFound(msg), BadRequest, Unauthorized, Forbidden, Conflict, Unprocessable, Internal, PreconditionFailed
func writeJSON(w, status, v)
func IdentityFromContext(ctx) (handle string, ok bool)
func MustHaveIdentity(r *http.Request) string  // 401 path handled by middleware
func requireTree(w, r, db, slug) bool          // returns true if it 404'd
func mountTrees, mountDecisions, mountState, mountAuditRoutes — already exist

--- existing CLI helpers (in internal/cli) ---
func openIndex(repoRoot) (*index.DB, error)
func requireDecisionsDir(repoRoot) error
func outputFormat(cmd) string  // honors local --output, falls back to root --output
func resolveDecisionID(db, query) (id string, err error)  // tier1=full id, tier2=prefix≥4, tier3=summary substr
func decisionToMap(d *Decision) map[string]any
func printDecision(cmd, d, format) error

--- MCP (mark3labs/mcp-go v0.50.0) ---
import mcpgo "github.com/mark3labs/mcp-go/mcp"
import server "github.com/mark3labs/mcp-go/server"
- server.NewMCPServer(name, version, opts...) → *server.MCPServer
- mcpgo.NewTool(name, opts ...) → mcpgo.Tool, opts: WithDescription, WithString/Array(name, opts), WithRequired
- s.AddTool(tool, handler func(ctx, mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error))
- mcpgo.NewResource(uri, name, opts...), mcpgo.NewResourceTemplate(uri, name, opts...)
- s.AddResource / AddResourceTemplate(resource, handler func(ctx, mcpgo.ReadResourceRequest) ([]mcpgo.ResourceContents, error))
- request.Params.Arguments = map[string]any (URI template variables)
- mcpgo.NewToolResultText(string), mcpgo.TextResourceContents{URI, MIMEType, Text}

