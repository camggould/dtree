import { describe, it, expect } from "vitest";
import { describeEvent, describeReason } from "@/util/auditDescribe";
import type { Decision, Event } from "@/api/types.gen";

// Minimal Decision factory — the describe functions only ever read a few
// fields, so we keep this tight rather than dragging the whole shape in.
function decision(overrides: Partial<Decision> = {}): Decision {
  return {
    id: "01D000000000000000000000A1",
    tree: "backend",
    summary: "test",
    creator: "alice",
    priority: "medium",
    status: "proposed",
    schema_version: 1,
    slug: "test",
    tags: [],
    ...overrides,
  } as Decision;
}

function event(overrides: Partial<Event> = {}): Event {
  return {
    event_id: "01EVT00000000000000000",
    v: 1,
    ts: "2026-05-03T12:00:00Z",
    actor: "alice",
    action: "create",
    kind: "decision",
    tree: "backend",
    id: "01D000000000000000000000A1",
    payload: {},
    ...overrides,
  } as Event;
}

describe("describeEvent", () => {
  it("returns null for unknown action", () => {
    expect(describeEvent(event({ action: "config_change" }), null)).toBeNull();
  });

  it("decide: followed recommendation with recommender name", () => {
    const ev = event({
      action: "decide",
      payload: {
        after: {
          actual_choice: "sqlc",
          is_recommended: true,
          recommended_summary: "sqlc",
          recommended_by: "bob",
        },
      },
    });
    expect(describeEvent(ev, null)).toBe(
      'chose “sqlc” (followed recommendation from bob)',
    );
  });

  it("decide: choice matches recommendation even without is_recommended flag", () => {
    const ev = event({
      action: "decide",
      payload: { after: { actual_choice: "Q3 invite, Q4 public" } },
    });
    const d = decision({
      recommended_summary: "Q3 invite, Q4 public",
      recommended_by: "alice",
    });
    expect(describeEvent(ev, d)).toBe(
      'chose “Q3 invite, Q4 public” (followed recommendation from alice)',
    );
  });

  it("decide: overrode existing recommendation", () => {
    const ev = event({
      action: "decide",
      payload: {
        after: {
          actual_choice: "GORM",
          recommended_summary: "sqlc",
          is_recommended: false,
        },
      },
    });
    expect(describeEvent(ev, null)).toBe(
      'chose “GORM” (overrode recommendation “sqlc”)',
    );
  });

  it("decide: no recommendation existed", () => {
    const ev = event({
      action: "decide",
      payload: { after: { actual_choice: "Fly.io" } },
    });
    expect(describeEvent(ev, null)).toBe(
      'chose “Fly.io” (no recommendation existed)',
    );
  });

  it("relate: renders src + verb + target summaries", () => {
    const src = decision({
      id: "01SRC00000000000000000",
      summary: "Choose primary database",
    });
    const tgt = decision({
      id: "01TGT00000000000000000",
      summary: "Choose ORM or query layer",
    });
    const map = new Map<string, Decision>();
    map.set(src.id, src);
    map.set(tgt.id, tgt);
    const ev = event({
      action: "relate",
      kind: "relationship",
      id: src.id,
      payload: { src: src.id, target: tgt.id, type: "blocks" },
    });
    expect(describeEvent(ev, null, map)).toBe(
      '“Choose primary database” blocks “Choose ORM or query layer”',
    );
  });

  it("relate: replaces underscores in relationship type", () => {
    const ev = event({
      action: "relate",
      payload: {
        src: "01SRC00000000000000000",
        target: "01TGT00000000000000000",
        type: "relates_to",
      },
    });
    const out = describeEvent(ev, null) ?? "";
    expect(out).toContain("relates to");
    expect(out).not.toContain("relates_to");
  });

  it("relate: falls back to short id when decision not in map", () => {
    const ev = event({
      action: "relate",
      payload: {
        src: "01ABCDEFGHIJKLMNOPQRSTUVWX",
        target: "01ZYXWVUTSRQPONMLKJIHGFEDC",
        type: "blocks",
      },
    });
    const out = describeEvent(ev, null) ?? "";
    expect(out).toContain("01ABCDEF");
    expect(out).toContain("01ZYXWVU");
    expect(out).toContain("blocks");
  });

  it("supersede: shows old → new summaries when both available", () => {
    const oldD = decision({ id: "01OLD00000000000000000", summary: "v1 pricing" });
    const newD = decision({ id: "01NEW00000000000000000", summary: "v2 pricing" });
    const map = new Map<string, Decision>();
    map.set(oldD.id, oldD);
    map.set(newD.id, newD);
    const ev = event({
      action: "supersede",
      id: oldD.id,
      payload: { old: oldD.id, new: newD.id },
    });
    expect(describeEvent(ev, null, map)).toBe(
      '“v1 pricing” superseded by “v2 pricing”',
    );
  });

  it("create: shows decision summary in quotes", () => {
    const ev = event({
      action: "create",
      payload: { after: { summary: "Choose authentication strategy" } },
    });
    expect(describeEvent(ev, null)).toBe('“Choose authentication strategy”');
  });

  it("tree_create: shows the title", () => {
    const ev = event({
      action: "tree_create",
      kind: "tree",
      tree: "",
      id: "backend",
      payload: { after: { title: "Backend Architecture", slug: "backend" } },
    });
    expect(describeEvent(ev, null)).toBe('“Backend Architecture”');
  });

  it("scope_out: returns reason when present", () => {
    const ev = event({
      action: "scope_out",
      payload: { after: { reason: "premature optimization" } },
    });
    expect(describeEvent(ev, null)).toBe("reason: premature optimization");
  });
});

describe("describeReason", () => {
  it("decide: returns actual_choice_reason from after", () => {
    const after = { actual_choice_reason: "Type-safe + we own the SQL" };
    expect(describeReason("decide", after, null)).toBe(
      "Type-safe + we own the SQL",
    );
  });

  it("decide: falls back to decision.actual_choice_reason", () => {
    const d = decision({ actual_choice_reason: "from parent" });
    expect(describeReason("decide", {}, d)).toBe("from parent");
  });

  it("scope_out: prefers payload reason over decision.out_of_scope_reason", () => {
    const d = decision({ out_of_scope_reason: "old reason" });
    const after = { reason: "new reason" };
    expect(describeReason("scope_out", after, d)).toBe("new reason");
  });

  it("returns null for actions with no reason concept", () => {
    expect(describeReason("create", {}, null)).toBeNull();
    expect(describeReason("relate", {}, null)).toBeNull();
  });
});
