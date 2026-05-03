---
name: dtree
description: Use dtree to track engineering decisions as plain YAML files with typed relationships and an audit log. Invoke when the user is making a non-trivial choice that warrants persistence (architecture, tooling, design tradeoff, scope cut, deferred work). Records decisions via the dtree CLI or MCP tools so future sessions and other contributors can see what was decided and why.
---

# dtree skill

Use this skill when working on a project that has a `.decisions/` directory at the repo root, OR when the user asks you to record / look up / decide something.

## What dtree does

dtree turns decisions into first-class artifacts. Each decision is a YAML file under `.decisions/<tree>/decisions/`. They have typed relationships, an append-only audit log, and a multi-actor model that distinguishes humans from agents.

You should treat dtree as the **system of record for "why we did X"**. Don't paraphrase prior decisions from memory — query dtree.

## When to use

**Record a decision** (`dtree new` or `create_decision`) when:
- A non-trivial choice is being made between alternatives.
- The user is about to do something irreversible (drop a feature, switch a dependency).
- You're about to make an assumption that will shape later work.

**Look up a decision** (`dtree show`, `dtree find`, `get_decision`) when:
- The user asks "why did we…" or "what did we decide about…".
- You're about to recommend something that touches a settled area.
- You need to understand the constraints around a piece of code.

**Add a recommendation** (`update_decision` or `dtree edit` with `--field recommended_*`) when:
- A decision is `proposed` and you have a position on it.
- You're an agent — your name will land in `recommended_by`, and the user can accept or override.

**Decide** (`dtree decide` or `decide_decision`) when:
- You have authority and the user has confirmed the call.
- Use `--is-recommended` (or `is_recommended: true`) when accepting an existing recommendation; this is the signal that gets tracked as agentic acceptance.

## Discover what exists

Before creating, check what's already there:

```sh
dtree status                          # quick overview: trees, counts, dirty index
dtree ls --status proposed            # what's open
dtree find "<keyword>"                # FTS5 search across all decisions
dtree show <id-prefix>                # full record (4+ chars unambiguous)
dtree graph deps <id>                 # what blocks this decision
dtree graph downstream <id>           # what this decision blocks
```

Via MCP, the equivalents are `list_decisions`, `find_decisions`, `get_decision`, and reading `dtree://trees/{tree}/decisions/{id}` resources.

## Identity

Every action is attributed. The user has a configured identity (`dtree whoami`); you have your own (typically `<user>-claude` or similar agent handle). When acting via MCP, the server is launched with `--as <handle>` and that's your identity for the session.

When deciding interactively from the CLI, prefer `--as <agent-handle>` to attribute clearly:

```sh
dtree as cam-claude new "Choose payment processor" --tree backend
```

## Schema essentials

```yaml
summary: short imperative title (≤200 chars; required)
priority: assumption | low | medium | high | critical
status:   proposed | decided | out_of_scope | superseded
creator:  handle of opener
recommended_by: handle of the recommender (often you, the agent)
recommended_summary: the choice you suggest
recommended_full: |
  why — options considered, constraints, tradeoffs
actual_choice: what was chosen (when decided)
actual_choice_reason: why the chosen option won
decided_by: [handles]
is_recommended: true if actual_choice matches recommended_summary
out_of_scope_reason: required when status=out_of_scope
decision_full_description: |
  long-form context for the decision itself —
  the question, the options, the constraints
relationships:
  - { type: blocks | influences | supersedes | relates_to, target: <id> }
```

**Critical:** `decision_full_description` is the long-form body — fill this in when you create a decision. Without it the queue and modal show "No description on this decision yet." which is a UX dead-end.

## Writing a good decision

A high-quality decision has:

1. **Clear summary** — what's being decided, in one short imperative line.
2. **Context (`decision_full_description`)** — the question, the options on the table, the constraints. 1–3 paragraphs is usually right.
3. **Recommendation when warranted** — both a one-liner (`recommended_summary`) and a justification (`recommended_full`). Skip the recommendation if you genuinely have no opinion.
4. **Relationships** — link `blocks` for hard dependencies, `influences` for soft signal. Always link related work.

Example via stdin:

```sh
cat <<'YAML' | dtree as cam-claude new --from-stdin --tree backend
summary: Choose authentication strategy
priority: high
tags: [auth, security]
recommended_summary: Magic links + WebAuthn upgrade path
recommended_full: |
  Magic links remove the password support burden and we already send
  transactional email. WebAuthn gives a low-friction step up for users
  who want stronger auth, without OAuth's IdP coupling.
recommended_by: cam-claude
decision_full_description: |
  How users sign in. Affects onboarding friction, account-recovery
  support load, and our compliance posture.

  Options:
  - OAuth (Google + GitHub) — fast for our ICP but couples us to the IdP
  - Magic links via email — zero password support burden
  - Password + TOTP — classic, max control, max support burden
  - External IdP (Auth0 / Clerk / WorkOS) — fastest to ship, ongoing $
YAML
```

## Acting on decisions

| Situation | Command |
|---|---|
| Take the recommendation as-is | `dtree decide <id> --by cam --is-recommended --choice "..." --reason "..."` |
| Override with your own choice | `dtree decide <id> --by cam --choice "..." --reason "..."` |
| Park for now | `dtree scope-out <id> --reason "..."` |
| Replace with a new decision | `dtree supersede <old-id> --by <new-id>` |
| Reverse a decision | `dtree undecide <id>` |
| Reverse a scope-out | `dtree restore <id>` |

Status guards are enforced: you can't `decide` a decision that's already decided, can't `undecide` a proposed one, etc. The server returns 409 Conflict; the CLI returns a clear error.

## Relationships discipline

- Use `blocks` sparingly — it's a hard ordering constraint that makes the target unactionable until the source is in a terminal state. Reserve for "we genuinely cannot answer X without first answering Y".
- Use `influences` for "X informs Y" without creating a hard dependency. Cheap, useful, makes the graph readable.
- Use `relates_to` for soft "see also" links. Doesn't affect layout much; pure annotation.
- `supersedes` is automatic when you run `dtree supersede`. Don't add it by hand.

Cycles in `blocks` are a bug. Run `dtree graph cycles` periodically.

## Recommendation acceptance — what gets measured

dtree's dashboard tracks how often each actor's recommendations get accepted vs overridden, split by who decided (self / another agent / a human). For you as an agent:

- **Acceptance rate by humans** is the trust signal. High = the user trusts your recommendations.
- **Self-acceptance** (you both recommend AND decide) is autonomous behaviour — usually fine for low-stakes choices, suspicious for high-stakes.
- **Overrides** are valuable feedback — when a human overrides your recommendation, look at `actual_choice_reason` to learn what you missed.

When you make a recommendation, your `recommended_by` is recorded. When the decision is later decided (by anyone), the system computes acceptance from `is_recommended` + `actual_choice` vs `recommended_summary`. Don't guess these — set `is_recommended: true` explicitly when you intend to accept the standing recommendation.

## Common patterns

### Pattern: "Why did we pick X?"

```sh
dtree find "X"                        # 0–N matches
dtree show <id>                       # full context including recommendation, outcome, reasoning
dtree audit ls --decision <id>        # who did what when, with reasons
```

### Pattern: "What's blocking us?"

```sh
dtree queue spearhead --tree backend  # decisions blocking the most others
dtree graph deps <id>                 # what specifically blocks this one
```

### Pattern: "What should I work on next?"

```sh
dtree queue quick-wins --tree backend # proposed, unblocked, ready to close
```

### Pattern: Capturing an assumption

When you take something as given without genuinely deciding, use `assume`:

```sh
dtree assume "Single-region deployment for v1" \
  --tree backend \
  --choice "us-east-1 only" \
  --reason "Multi-region adds weeks of work for capacity we won't need until 1k+ DAU. Revisit at v2."
```

This lands as `status=decided` with `priority=assumption` — visible in the graph and audit log, but excluded from active queues. The UI gives it a distinct grey/dashed treatment.

## Don't

- Don't paraphrase prior decisions from memory in a long-running session — query dtree (`dtree show <id>` or `find_decisions`) to be sure.
- Don't decide on the user's behalf without confirmation. Recommend, don't decide, unless explicitly asked.
- Don't skip `decision_full_description` — it's the only place future readers learn what you were actually weighing.
- Don't use `blocks` for "would be nice to do first". Reserve for hard ordering. Use `influences` otherwise.
- Don't rely on the index DB — it's rebuildable. Source of truth is the YAML + JSONL on disk.
- Don't manually edit the index DB or audit JSONL — go through the CLI / API so events are recorded.

## Read-only mode

If launched with `--read-only`, the MCP server only exposes query tools (`list_*`, `get_*`, `find_*`, `decision_history`) and refuses mutations. Don't try mutations in read-only mode; they'll error and the user explicitly chose this for safety.

## Quick reference

| Goal | CLI | MCP tool |
|---|---|---|
| Find decisions | `dtree find "..."` | `find_decisions` |
| Show one | `dtree show <id>` | `get_decision` |
| Create | `dtree new "..."` | `create_decision` |
| Decide | `dtree decide <id> ...` | `decide_decision` |
| Override | `dtree decide <id> --choice ... --reason ...` | `decide_decision` |
| Park | `dtree scope-out <id> --reason ...` | `scope_out_decision` |
| Replace | `dtree supersede <old> --by <new>` | `supersede_decision` |
| Link | `dtree relate <a> blocks <b>` | `relate_decisions` |
| History | `dtree audit ls --decision <id>` | `decision_history` |
| List trees | `dtree tree list` | `list_trees` (resource: `dtree://trees`) |

For exhaustive flag details, run `dtree <command> --help`.
