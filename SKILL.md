---
name: dtree
description: Use dtree to record the choices you make while working — architectural decisions, tooling picks, scope cuts, deferred work, assumptions you're operating under. Invoke when you're about to commit to a non-obvious choice, when the user asks why something was decided, or when you want to leave a recoverable trail of your reasoning. Records via the dtree CLI (preferred) or MCP tools.
---

# dtree skill

dtree turns the decisions you make while working into a first-class artifact. Each decision is a small YAML file under `.decisions/`, with a typed graph of relationships (`blocks`, `influences`, `supersedes`, `relates_to`) and an append-only audit log. Future you, future agents, and the user can all replay the reasoning.

Use this skill when:
- you're about to commit to a non-obvious technical choice
- you're about to make an assumption that shapes downstream work
- the user asks "why did we do X?" or "what did we decide about Y?"
- you're reviewing code or plans and want to log a finding
- you want the user to be able to override or audit your past calls

## Detect whether the project uses dtree

Look for `.decisions/` at the repo root. If it doesn't exist, dtree isn't initialised here — don't proactively initialise it; ask the user first. If it does exist, the rest of this skill applies.

## Pick your identity

Every action in dtree is attributed to a *handle* — a stable, registered identity. You should have your own, distinct from the user's:

```sh
dtree actor add claude --name "Claude" --kind agent --email noreply@anthropic.com
```

Or whatever name the user prefers (`claude-on-cam-laptop`, `agent-reviewer`, etc.). The handle should be stable — reuse the same one across sessions so your activity aggregates.

Three ways to act under your handle (in order of preference):

1. **`--as <handle>` flag** for one-shot use (no config needed, never wrong):
   ```sh
   dtree as claude new "Pick test framework" --tree backend
   dtree as claude decide 01HX --choice ... --reason ... --by claude
   ```
2. **`DTREE_AS=claude` env var** for a session — set it once at the start, every subsequent `dtree …` is attributed to you.
3. **Local config**: `dtree config set --local identity.default claude` writes the project's `.decisions/config.yaml`. Use only if the project is yours alone; otherwise it'll override the user's identity.

If you forget to set identity, `dtree` falls back to whatever the user has configured globally — your activity will be attributed to *them*, which is wrong. Always be explicit.

Verify: `dtree as claude whoami` should print your handle.

## Discover what exists before creating

Before writing a new decision, check for an existing one:

```sh
dtree status                          # quick repo overview
dtree ls --status proposed            # what's already open
dtree find "<keyword>"                # FTS5 search across all decisions
dtree show <id-prefix>                # full record (4+ chars unambiguous)
dtree graph deps <id>                 # what blocks this decision
dtree graph downstream <id>           # what this decision blocks
dtree audit ls --decision <id>        # full timeline for one decision
```

Via MCP: `list_decisions`, `find_decisions`, `get_decision`, `decision_history`, plus reading `dtree://trees/{tree}/decisions/{id}` resources.

If a decision already covers what you were about to record, *update or relate to it* instead of creating a duplicate.

## Decision schema (essentials)

```yaml
summary:    Imperative one-liner — what's being decided (≤200 chars; required)
priority:   assumption | low | medium | high | critical
status:     proposed | decided | out_of_scope | superseded
creator:    handle of opener (you, when you create)
recommended_by:      handle who's recommending — set this to YOUR handle
                     when you make a recommendation
recommended_summary: the choice you suggest (short)
recommended_full: |
  Why — options considered, constraints, tradeoffs.
actual_choice:           what was chosen (set when status=decided)
actual_choice_reason:    why the chosen option won
decided_by:              [handles] of everyone party to the call
is_recommended:          true if actual_choice matches recommended_summary
out_of_scope_reason:     required when status=out_of_scope
decision_full_description: |
  Long-form context for the decision itself — the question, the options,
  the constraints. THIS IS THE BODY of the decision. Without it, future
  readers see no context. Always write it.
relationships:
  - { type: blocks | influences | supersedes | relates_to, target: <id> }
```

**`decision_full_description` is non-optional in practice.** Empty descriptions kill the value of the audit trail; future agents (and humans) need to know what you were weighing.

## Three roles, three flows

You can play any of three roles on a single decision. Be intentional about which.

### Role 1 — Creator: opening the question

Use when something needs to be decided but hasn't been formalised yet.

```sh
cat <<'YAML' | dtree as claude new --from-stdin --tree backend
summary: Choose authentication strategy
priority: high
tags: [auth, security]
decision_full_description: |
  How users sign in. Affects onboarding friction, account-recovery
  load, and our compliance posture.

  Options considered:
  - OAuth (Google + GitHub) — fast for our ICP, IdP coupling
  - Magic links via email — zero password support burden
  - Password + TOTP — classic, max control, max support
  - External IdP (Auth0/Clerk/WorkOS) — fastest to ship, ongoing $

  Constraint: we already send transactional email, so the channel
  is set up.
recommended_by: claude
recommended_summary: Magic links + WebAuthn upgrade
recommended_full: |
  Magic links remove the password support burden and we already
  have the email channel. WebAuthn gives a low-friction step-up
  for users who want stronger auth, without OAuth's IdP coupling.
YAML
```

You're both creator and recommender here. The user (or another decider) gets to accept or override.

### Role 2 — Recommender: weighing in on someone else's open question

Use when there's already a `proposed` decision and you have a position. Set `recommended_*` fields so the user sees your suggestion + reasoning when they look at the decision.

```sh
dtree as claude edit 01HX \
  --field recommended_summary="sqlc" \
  --field recommended_full="Type-safe Go from hand-written SQL. We own the SQL, no runtime reflection, refactors caught at compile time." \
  --field recommended_by=claude
```

(Or via MCP: `update_decision` with `recommended_summary`/`recommended_full`/`recommended_by` in `fields`.)

### Role 3 — Decider: making the actual call

Be cautious here. **Do not decide on the user's behalf without explicit authorisation.** Two patterns:

**A — confirm the recommendation:** when the user says "yes, take your suggestion":
```sh
dtree as claude decide 01HX --by claude \
  --choice "Magic links + WebAuthn upgrade" \
  --reason "Approved by user — see chat thread X." \
  --is-recommended
```

**B — record an autonomous low-stakes call:** for choices the user has delegated to you (e.g. test runner names, internal helper names) that you've decided unilaterally:
```sh
dtree as claude decide 01HX --by claude --choice "..." --reason "..."
```
Skip `--is-recommended` if you went a different way than your own recommendation.

Status guards are enforced. You can't `decide` something already decided, can't `undecide` something not decided. The CLI returns a clear error.

## Capturing assumptions

When you're operating under a premise rather than choosing between alternatives, log it as an assumption — a "we'll go with X for now, revisit later" marker:

```sh
dtree as claude assume "Single-region deployment for v1" \
  --tree backend \
  --choice "us-east-1 only" \
  --reason "Multi-region adds weeks of work for capacity we won't need until 1k+ DAU. Revisit at v2."
```

Assumptions land as `status=decided` with `priority=assumption` — visible in the graph and audit log, excluded from active queues, distinct grey/dashed treatment in the UI. Use freely; they're cheap.

## Relationships discipline

| Type | When to use |
|---|---|
| `blocks` | Hard ordering constraint — target genuinely cannot be acted on until source is in a terminal state. Reserve for "we cannot answer X without first answering Y". |
| `influences` | Soft signal — "X informs Y". Cheap, useful, makes the graph readable. Use liberally. |
| `relates_to` | "See also" — pure annotation. |
| `supersedes` | Don't add by hand; `dtree supersede` does it for you. |

Cycles in `blocks` are a bug. Run `dtree graph cycles` periodically.

## Patterns

### "Why did we pick X?"

```sh
dtree find "X"                        # 0–N matches
dtree show <id>                       # full context: rec, outcome, reason
dtree audit ls --decision <id>        # who did what when, with reasons
```

### "What's blocking us?"

```sh
dtree queue spearhead                 # decisions blocking the most others
dtree graph deps <id>                 # what specifically blocks one decision
```

### "I'm about to write code that touches a settled area"

Search first. Honour the decision unless you have new information that warrants reopening it (in which case `dtree undecide` + add your update to `decision_full_description` + re-decide, OR create a new decision and `dtree supersede` the old one).

### "I'm reviewing a PR / plan and want to log a finding"

Create a decision with `priority=medium`, status=`proposed`, set yourself as `recommended_by`, and link to the related decisions via `influences`:

```sh
dtree as claude new "Should we add input validation to /v1/foo?" \
  --tree backend --priority medium
# then add a relationship to the decision that introduced the endpoint
dtree relate <new-id> influences <foo-endpoint-decision-id>
```

## What gets measured

The dashboard tracks every actor's:
- **Recommendation acceptance rate** — split by who decided (self / another agent / human). The "human accepted my rec" rate is the real trust signal.
- **Decision count** — what you've directly decided.
- **Self-acceptance** — when you both recommend AND decide. Acceptable for low-stakes; suspicious for high-stakes.

Knowing this means: if you make a recommendation and want it tracked as accepted, set `--is-recommended` when deciding (or `is_recommended: true` via the API). Don't set it unless the choice actually matches the recommendation — the metric depends on the truth.

## Don't

- Don't decide on the user's behalf without confirmation. **Recommend, don't decide.** Exception: low-stakes calls the user has explicitly delegated to you.
- Don't paraphrase prior decisions from memory in a long-running session — query dtree.
- Don't skip `decision_full_description`. It's the only place future readers learn what you were weighing.
- Don't use `blocks` for "would be nice to do first" — reserve for hard ordering. Use `influences` otherwise.
- Don't manually edit the YAML or JSONL on disk — go through the CLI / MCP so events are recorded in the audit log.
- Don't create a decision for every small choice. Reserve dtree for things that have *future cost if forgotten*. A line of refactoring doesn't need one; switching a dependency does.

## Read-only mode

If launched as `dtree mcp --read-only`, only query tools work (`list_*`, `get_*`, `find_*`, `decision_history`). Mutations are refused. Don't try them — the user picked read-only for a reason.

## Quick reference

| Goal | CLI | MCP tool |
|---|---|---|
| Find decisions | `dtree find "..."` | `find_decisions` |
| Show one | `dtree show <id>` | `get_decision` |
| Open the question | `dtree as <you> new "..."` | `create_decision` |
| Record assumption | `dtree as <you> assume "..." --choice ... --reason ...` | `create_decision` (priority=assumption, status=decided) |
| Add a recommendation | `dtree edit <id> --field recommended_*` | `update_decision` |
| Accept a recommendation | `dtree as <you> decide <id> --is-recommended ...` | `decide_decision` (`is_recommended: true`) |
| Override / decide own way | `dtree as <you> decide <id> --choice ... --reason ...` | `decide_decision` |
| Park | `dtree scope-out <id> --reason ...` | `scope_out_decision` |
| Replace | `dtree supersede <old> --by <new>` | `supersede_decision` |
| Reverse a decision | `dtree undecide <id>` | `undecide_decision` |
| Link | `dtree relate <a> blocks <b>` | `relate_decisions` |
| History | `dtree audit ls --decision <id>` | `decision_history` |
| Trees | `dtree tree list` | `list_trees`, resource `dtree://trees` |
| Your identity | `dtree as <you> whoami` | (set at server launch via `--as`) |

For exhaustive flag details, run `dtree <command> --help`.
