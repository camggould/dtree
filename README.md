# dtree

> A directory-based persistence layer for building, recording, and auditing decisions.

[![Status: alpha](https://img.shields.io/badge/status-alpha-orange.svg)](#status)

`dtree` lets engineering teams record decisions as plain YAML files in a directory structure, track relationships between them (blocks, influences, supersedes), maintain a complete audit log, and surface the result through a CLI, a local web UI, and an MCP server for AI agents.

## Status

**Alpha — in active development.** The design is settled (see PRD), implementation is underway. APIs and storage formats may shift before v1.0.

## What is dtree?

Most teams scatter decision-making across Slack threads, ad-hoc docs, and engineers' heads. Six months later, no one remembers why a decision was made — or even that it was made.

`dtree` makes decisions a first-class artifact:

- **Directory-based.** Each "decision tree" is a directory under `.decisions/`. Each decision is a YAML file. Plain text, version-controlled by your existing git workflow.
- **Auditable.** Every mutation (create, update, decide, scope-out, supersede) is recorded in an append-only JSONL audit log. Replay state at any point in time.
- **Relational.** Decisions can `block`, `influence`, `supersede`, or `relate_to` other decisions. Visualize as a DAG.
- **Multi-identity.** Track who created, who decided, who recommended. Distinguish humans from AI agents. Measure how often agent recommendations get accepted.
- **CLI, web UI, and MCP.** Use `dtree` from the terminal, browse decisions in a polished local web UI (`dtree ui`), or expose tools to AI agents (`dtree mcp`).

## Installation

### From source

```sh
git clone https://github.com/cgould/dtree.git
cd dtree
make build
sudo install -m 0755 dtree /usr/local/bin/dtree
```

### Via `go install`

```sh
go install github.com/cgould/dtree/cmd/dtree@latest
```

### Pre-built binary (when releases are available)

```sh
curl -L https://github.com/cgould/dtree/releases/latest/download/dtree-$(uname -s)-$(uname -m) -o /usr/local/bin/dtree
chmod +x /usr/local/bin/dtree
```

Verify:

```sh
dtree version
```

## Quickstart

```sh
# 1. One-time machine setup (first run)
dtree config set --global identity.default cam

# 2. In your project repo
dtree init                                    # creates .decisions/
dtree tree create backend                     # create a decision tree
dtree actor add cam --name "Cam" --email cam@example.com

# 3. Create a decision (opens $EDITOR with a template)
dtree new "Pick database engine" --tree backend

# 4. List proposed decisions
dtree ls --status proposed

# 5. Decide it
dtree decide 01HXKQ5Z --by cam --choice "SQLite + FTS5" \
  --reason "Single-binary requirement; FTS fits."

# 6. Open the web UI
dtree ui

# 7. Expose MCP tools to your AI assistant
dtree mcp --as cam-claude
```

## Concepts

### Decision tree

A named collection of decisions. Stored as a subdirectory under `.decisions/`. Most projects have one or two trees; teams with multiple workstreams may have more.

### Decision

A YAML file capturing a single decision. Required: a summary, priority, status. Optional: recommendation, outcome, relationships, tags.

```yaml
id: 01HXKQ5Z3PCWJ8FQR4M2TVB7D9
summary: Pick database engine
priority: high
status: decided
creator: cam
recommended_summary: SQLite + FTS5
recommended_by: cam-claude
actual_choice: SQLite + FTS5
actual_choice_reason: Single-binary requirement is non-negotiable.
decided_by: [cam, alice]
is_recommended: true
relationships:
  - type: blocks
    target: 01HXKQ7N9MR4VXBPDTYFW2K8H1
```

### Status

| | Meaning |
|---|---|
| `proposed` | Default after creation; not yet decided |
| `decided` | An `actual_choice` has been recorded |
| `out_of_scope` | Explicitly declined; not going to be decided |
| `superseded` | Replaced by another decision |

### Priority

`assumption` · `low` · `medium` · `high` · `critical`

`assumption` is a low-ceremony "we're going to take this as given" — recorded but excluded from action queues.

### Relationship types

| | Meaning |
|---|---|
| `blocks` | Target cannot be decided until source is in a terminal state |
| `influences` | Source informs target's outcome |
| `supersedes` | Source replaces target (drives target → `superseded`) |
| `relates_to` | Weak relatedness, no constraint |

### Identity

Every action is attributed to a *handle* — a stable, registered identity. Handles can be `human` or `agent` (LLM). Identities are configured globally (`~/.config/dtree/config.yaml`) and registered per-project (`.decisions/actors.yaml`).

## Usage guide

### Initialize a project

```sh
dtree init                          # creates .decisions/ and prompts to register your identity
dtree tree create <name>            # create your first decision tree
```

### Create decisions

```sh
dtree new "Summary"                                       # opens $EDITOR with a template
dtree new "Summary" --priority high --tag storage         # inline flags
dtree new "Summary" --from-file draft.yaml                # full YAML body
echo '...' | dtree new "Summary" --from-stdin             # via stdin
dtree assume "Users have stable IDs"                      # shortcut for assumption-priority
```

### View decisions

```sh
dtree ls                                  # default: proposed + decided
dtree ls --status proposed --priority high,critical
dtree ls --tag storage --since 30d
dtree show <id>                           # full decision
dtree find "database"                     # FTS5 search
```

### Decide

```sh
dtree decide <id> --choice "..." --reason "..." --by cam
dtree decide <id> --by cam --by alice --is-recommended    # accept the recommendation
dtree undecide <id>                                       # back to proposed
dtree scope-out <id> --reason "Not relevant after pivot"
dtree supersede <old-id> <new-id>
```

### Relationships

```sh
dtree relate <src> blocks <target>
dtree relate <src> influences <target>
dtree unrelate <src> blocks <target>
```

### Graph queries

```sh
dtree graph deps <id>                     # what blocks this?
dtree graph closure <id> --type blocks --depth 3
dtree graph cycles
dtree graph viz <id> --format dot | dot -Tsvg -o tree.svg
```

### Queues (for guided workflow)

```sh
dtree queue spearhead                     # critical/high, unblocked, sorted
dtree queue spearhead --next              # just the first item
dtree queue quick-wins                    # low/medium, unblocked
```

### Audit

```sh
dtree audit ls                            # all events
dtree audit ls --actor cam --since 7d
dtree audit ls --decision <id>            # one decision's history
dtree audit show <event-id>
dtree audit replay --at "2026-04-22T14:32:11Z"
```

### Identity & config

```sh
dtree whoami                              # show resolved identity
dtree config set --global identity.default cam
dtree config set --local default_tree backend
dtree config list
dtree as cam-claude new "..."             # one-shot identity override
```

### Servers

```sh
dtree serve                               # HTTP API on 127.0.0.1:auto-port
dtree serve --listen 0.0.0.0:8080         # network bind (requires tokens)
dtree ui                                  # serve + open browser
dtree mcp --as cam-claude                 # MCP stdio for agents
dtree mcp --as cam-claude --read-only     # safe-mode
```

### Repo maintenance

```sh
dtree status                              # repo health
dtree reindex                             # rebuild SQLite index
dtree sync                                # reconcile external file edits
dtree fsck                                # validate invariants
dtree migrate                             # run schema migrations
```

## Configuration

### Layered config (git-style)

| Layer | Path |
|---|---|
| Global | `~/.config/dtree/config.yaml` |
| Local | `.decisions/config.yaml` |
| Env | `DTREE_AS`, `DTREE_TREE` |
| Flags | `--as`, `--tree` |

Higher layers override lower. `dtree config get <key>` shows the resolved value and source layer.

### Common keys

```yaml
identity:
  default: cam
editor: $EDITOR        # falls back to vi
output: human          # human | json | yaml
color: auto            # auto | always | never
default_tree: backend  # used when --tree omitted (single-tree repos)
```

## Web UI

```sh
dtree ui
```

Features:
- **Graph view** — interactive DAG with auto-layout (dagre). Click nodes for details.
- **Decision detail** — view, edit, decide. Per-decision audit history as a flowchart.
- **Kanban** — read-only swimlane visualization by status.
- **Queues** — guided "next decision" flow for non-technical users (Quick Wins / Spearhead).
- **Audit log** — searchable, filterable event table.
- **Timeline scrubber** — drag through time to see graph state at any point.
- **Dashboard** — metrics: decision counts, decider activity, recommendation acceptance.

The UI is served from `localhost` only by default. The same server runs headless via `dtree serve`.

## MCP integration (AI agents)

`dtree mcp` exposes tools and resources via the Model Context Protocol. Add to Claude Desktop's `mcpServers` config:

```json
{
  "mcpServers": {
    "dtree": {
      "command": "dtree",
      "args": ["mcp", "--as", "cam-claude"]
    }
  }
}
```

Tools include `dtree_create_decision`, `dtree_decide`, `dtree_search_decisions`, `dtree_list_audit`, `dtree_replay_state`, etc. All mutations are attributed to the handle the server was launched as, recorded in the audit log with `kind: agent`.

Use `--read-only` to give agents query-only access.

## Developer guide

### Prerequisites

- Go 1.22+
- Node 20+ and pnpm 9+ (for the UI)
- `make`

### Setup

```sh
git clone https://github.com/cgould/dtree.git
cd dtree
make setup       # installs Go deps + UI deps + generates types
```

### Development workflow

```sh
make dev         # runs go server + vite dev server with hot reload
                 # Server: http://127.0.0.1:8080
                 # UI dev: http://127.0.0.1:5173 (proxies API to :8080)

make api         # regenerate ui/src/api/types.ts from Go structs (after Go changes)
make build       # production single binary with embedded UI
make test        # go test + vitest
make lint        # biome + go vet
```

### Project structure

```
cmd/dtree/                  # CLI entrypoint
internal/
  core/                     # domain types, validation
  storage/                  # YAML, JSONL, SQLite
  audit/                    # event sourcing
  ulid/                     # ID generation
  cmd/                      # cobra command handlers
  server/                   # HTTP server
  mcp/                      # MCP server
  ui/                       # //go:embed for the SPA
  migrations/               # schema migration code
ui/                         # frontend source (Vite + React + TS)
```

### Type sharing

Go is the source of truth. After modifying Go structs that are part of the API surface:

```sh
make api
git add ui/src/api/types.ts
```

CI fails if generated `types.ts` would change without a commit.

### Storage format

- Decisions: YAML with structured fields and block-scalar prose
- Audit log: append-only JSONL, monthly-partitioned, `merge=union` for distributed contributors
- Index: SQLite with WAL mode, gitignored, fully rebuildable

See `PRD.md` for full architectural detail (not in git; generated locally).

### Contributing

Issues tracked via [beads](https://github.com/gastownhall/beads):

```sh
bd ready                   # find unblocked work
bd show bd-<n>             # task detail
bd close bd-<n> --reason "Implemented in PR #X"
```

Pull requests welcome. Each PR should:

1. Address a single bd issue
2. Include tests (Go for backend, Vitest for UI)
3. Update generated artifacts (`make api` if API surface changed)
4. Pass `make lint test`

### License

TBD
