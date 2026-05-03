=== UI VIEWS BRIEF (Phase C) ===

Read AFTER `cat .claude/briefs/ui_endpoints.md` for the API surface + types.

WHAT EXISTS (built in Phase B — DO NOT recreate):
- `ui/src/api/client.ts` — apiFetch<T>(path, init?) → {data, etag}; throws ApiError
- `ui/src/api/query.ts` — QueryClient + `keys` registry + hooks: useTrees, useDecisions, useDecision, useMetrics
- `ui/src/api/sse.ts` — useAuditStream({tree?})
- `ui/src/api/types.gen.ts` — TS types generated from internal/core
- `ui/src/store/app.ts` — Zustand: identity (currentHandle), prefs (theme), session (lastTreeSlug)
- `ui/src/components/IdentitySelector.tsx` — nav dropdown
- `ui/src/components/Layout.tsx` — Navbar + sidebar + outlet
- Routes wired in `ui/src/App.tsx` with stub views you'll replace

LIBRARIES already installed:
- @heroui/react v2 (NextUI rebrand) — Card, Button, Input, Tabs, Table, Modal, Dropdown, Spinner, Chip, Tooltip, etc.
- framer-motion — for HeroUI animations
- lucide-react — icons
- @tanstack/react-query v5
- zustand v5
- wouter — routing + useSearch for query strings
- vitest

Add as needed:
- `reactflow` + `@dagrejs/dagre` — graph view
- `recharts` — dashboard charts
- `@tanstack/react-table` v8 — audit table
- `date-fns` — relative timestamps

=== KEY PATTERNS ===

**Adding a new query hook**: extend `query.ts` `keys` registry then write `useFoo()` calling `useQuery({queryKey: keys.foo(...), queryFn: () => apiFetch<...>('/v1/...')})`

**Mutations**: write hooks like `useDecide(tree, id)` returning `useMutation({mutationFn: (body) => apiFetch(..., {method: 'POST', body: JSON.stringify(body)}), onSuccess: () => qc.invalidateQueries({queryKey: keys.decision(tree, id)})})`

**Identity in mutations**: `apiFetch` already injects `X-Dtree-As`. Don't add it manually.

**Optimistic concurrency**: pass `ifMatch: decision.rev` in PATCH/DELETE/lifecycle calls. On 412, refetch + show conflict toast.

**Filter state in URL**: use `wouter`'s `useSearch()` for query string. Helper:
```ts
const [search, setSearch] = useSearch();
const params = new URLSearchParams(search);
const status = params.get('status');
```

**HeroUI theme**: `<HeroUIProvider>` wraps App. Theme toggle reads `prefs.theme` from store.

=== VIEWS TO BUILD (Phase C) ===

Each view replaces a stub in `ui/src/views/`. Spawn ONE agent per view (or bundle 2-3 if they share components).

**Graph view (lth.12)** — `/trees/:tree`
- ReactFlow canvas. Nodes = decisions (status-colored). Edges = relationships (typed by color). Use dagre layout (TB).
- Click node → opens detail panel (right drawer).
- Toolbar: filter pills (status, priority, tag), layout toggle (dagre vs free), zoom controls.
- Hook: `useDecisions(tree, filters)`.

**Decision detail panel (lth.11)** — opens over current view (drawer or modal)
- Tabs: Overview / History / Relationships / Audit.
- Overview: editable summary/description, lifecycle action buttons (Decide/ScopeOut/Supersede/Restore based on status). Mutations via the hooks above.
- History tab: timeline of events from `useHistory(tree, id)` (you'll write this hook).
- Relationships: list with relate/unrelate buttons.
- Audit: per-decision audit flow (see lth.13).

**Filter pills (lth.10)** — `ui/src/components/FilterPills.tsx`
- Reusable across Graph, Kanban, Queue views.
- Each pill: HeroUI `Chip` with close button. Add via `Dropdown` of options.
- State lives in URL query string.

**Per-decision audit flowchart (lth.13)** — sub-component used in detail panel
- ReactFlow LR layout: each event = node, time-ordered left to right.
- Node label: action + actor + relative time.

**Tree-wide audit table (lth.14)** — `/trees/:tree/audit`
- @tanstack/react-table with columns: ts, actor, action, kind, ref, summary.
- Filter inputs above the table.
- Pagination via cursor (server-side).
- SSE live tail toggle (uses `useAuditStream`).

**Queue views (rn0.2, rn0.3)** — `/trees/:tree/queue/:kind`
- Switches on `:kind` — quick-wins or spearhead.
- Calls `useQueue(tree, kind)` (write hook).
- Renders ranked list with summary + counters + "open detail" action.

**Kanban view (rn0.1)** — `/trees/:tree/kanban`
- Read-only swimlanes by status (proposed / decided / out_of_scope / superseded).
- HeroUI Card per decision. Click → detail panel.
- NOT drag-and-drop (per PRD).

**Dashboard (rn0.5-8)** — `/dashboard`
- Recharts: status pie, priority bar, activity-over-time line, top contributors.
- "Recommendation acceptance" headline metric: % of decided where actual_choice matches recommended_summary or is_recommended=true.
- Cross-tree (no `:tree` param). Aggregates via per-tree `useMetrics(slug)` hooks.

**Timeline scrubber (rn0.4)** — overlays graph view
- Slider over event timestamps; drag to call `useState({tree, at: ts})` for state replay.
- Uses GET /v1/trees/{tree}/state?at=... endpoint.

**Polish (rn0.9)** — final pass:
- Keyboard shortcuts (cmd-k for command palette, j/k for list nav, esc to close detail).
- Focus rings, empty states ("No decisions yet — create one"), error boundaries with retry.
- Loading states everywhere (HeroUI Spinner).
- Dark/light theme verification.
