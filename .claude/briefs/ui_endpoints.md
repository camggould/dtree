=== DTREE HTTP API SURFACE (for UI consumers) ===

BASE: /v1
AUTH:
  - localhost trust (default): send `X-Dtree-As: <handle>` header.
  - token trust (network bind): send `Authorization: Bearer <token>` header.
ETag: GET single decision/tree returns ETag = current rev. PATCH/DELETE/lifecycle
   actions accept `If-Match: <rev>` for optimistic concurrency; on mismatch
   server returns 412 Precondition Failed (RFC 7807 Problem body).

ERROR FORMAT (RFC 7807 Problem Details):
   { "type":"about:blank", "title":"...", "status":N, "detail":"...", "instance":"..." }

PAGINATION: list endpoints accept ?limit=N&cursor=<opaque>; response is
   { "items": [...], "next_cursor": "..." } when more results exist.

--- Health ---
GET  /v1/health                              → {version, status: "ok"}

--- Trees ---
GET  /v1/trees[?include_archived=true]       → {trees: [Tree]}
POST /v1/trees                               body Tree → 201 Tree
GET  /v1/trees/{slug}                        → Tree (ETag set)
PATCH /v1/trees/{slug}                       merge patch → Tree
DELETE /v1/trees/{slug}                      204 (refuses if has decisions; pass ?force=true)

--- Decisions ---
GET  /v1/trees/{tree}/decisions              query: status, priority, tag, creator,
                                               decider, assigned, search, limit, cursor
                                             → {items: [Decision], next_cursor?}
POST /v1/trees/{tree}/decisions              body Decision → 201 Decision (ETag)
GET  /v1/trees/{tree}/decisions/{id}         → Decision (ETag = rev)
PATCH /v1/trees/{tree}/decisions/{id}        merge patch + If-Match → Decision
DELETE /v1/trees/{tree}/decisions/{id}[?hard=true] → 204
POST /v1/trees/{tree}/decisions/{id}/decide      body {choice, reason, by[], is_recommended?}
POST /v1/trees/{tree}/decisions/{id}/undecide
POST /v1/trees/{tree}/decisions/{id}/scope-out   body {reason}
POST /v1/trees/{tree}/decisions/{id}/supersede   body {by: <new-id>}
POST /v1/trees/{tree}/decisions/{id}/restore
POST /v1/trees/{tree}/decisions/{id}/relate      body {type, target, note?}
POST /v1/trees/{tree}/decisions/{id}/unrelate    body {type, target}
GET  /v1/trees/{tree}/decisions/{id}/history     → {events: [Event]}

--- State replay ---
GET  /v1/trees/{tree}/state[?at=RFC3339]     → {as_of, decisions: [Decision]}

--- Audit ---
GET  /v1/audit                                query: tree, ref, since, until, action, limit, cursor
                                              → {items: [Event], next_cursor?}
GET  /v1/audit/{event_id}                     → Event
GET  /v1/audit/stream                         text/event-stream (SSE) — NEW events as JSON
GET  /v1/audit/export[?format=jsonl]          export blob

--- Actors ---
GET  /v1/actors[?include_archived=true]       → {actors: [Actor]}
GET  /v1/actors/{handle}                      → Actor
POST /v1/actors                               body Actor → 201
PATCH /v1/actors/{handle}                     merge patch → Actor
POST /v1/actors/{handle}/archive
POST /v1/actors/{handle}/rename               body {new_handle}

--- Queues ---
GET  /v1/trees/{tree}/queues/spearhead?limit=N    → {items: [{id, summary, blocking_count}]}
GET  /v1/trees/{tree}/queues/quick-wins?limit=N   → {items: [Decision]}
GET  /v1/trees/{tree}/queues/unassigned?limit=N   → {items: [Decision]}

--- Metrics ---
GET  /v1/trees/{tree}/metrics
   → { total_decisions, by_status: {<status>: N}, by_priority: {<priority>: N},
       by_creator: [{handle, count}], assumptions_count, unblocked_proposed_count,
       oldest_proposed_id }

=== TYPESCRIPT TYPES (mirror Go core types — generate via tygo) ===

type Status = "proposed" | "decided" | "out_of_scope" | "superseded";
type Priority = "assumption" | "low" | "medium" | "high" | "critical";
type RelationshipType = "blocks" | "influences" | "supersedes" | "relates_to";
type ActorKind = "human" | "agent";
type Action = "create" | "update" | "delete" | "decide" | "undecide" | "scope_out" |
              "supersede" | "relate" | "unrelate" | "rename" | "restore" |
              "external_edit" | "external_create" | "external_delete" |
              "tree_create" | "tree_delete" | "tree_rename" | "tree_archive" |
              "actor_add" | "actor_rename" | "actor_archive" |
              "config_change" | "schema_migrate";
type Kind = "decision" | "tree" | "actor" | "relationship" | "config";

interface Relationship { type: RelationshipType; target: string; note?: string }
interface Decision {
  id: string; tree: string; slug?: string;
  summary: string; description?: string;
  priority: Priority; status: Status;
  tags?: string[]; creator: string; assignee?: string;
  recommended_summary?: string; recommended_full?: string; recommended_by?: string;
  is_recommended?: boolean;
  actual_choice?: string; actual_choice_reason?: string;
  decided_by?: string[]; decided_at?: string;  // RFC3339
  created_at: string; updated_at: string;
  relationships?: Relationship[];
  rev: string;
}
interface Tree { slug: string; name: string; description?: string; archived: boolean; created_at: string; updated_at: string }
interface Actor { handle: string; display_name: string; kind: ActorKind; email?: string; archived: boolean }
interface Event {
  event_id: string; v: number; ts: string;
  actor: string; action: Action; kind: Kind;
  tree?: string; id: string;
  payload: { before?: Record<string, any>; after?: Record<string, any>; [k: string]: any }
}
