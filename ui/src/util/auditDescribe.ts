// Plain-language descriptions for audit events. Shared by the per-decision
// History tab in DecisionModal and the cross-tree Recent Activity panel on
// HomeView so both surfaces describe the same event identically.

import type { Decision, Event } from "@/api/types.gen";

/** Cap for inline reasoning quotes. Longer text is truncated; the full
 *  text remains on the parent decision. */
export const REASON_CAP = 240;

/** Truncate text and append an ellipsis. Whitespace-aware. */
export function truncate(s: string, max: number): string {
  if (s.length <= max) return s;
  return s.slice(0, max).trimEnd() + "…";
}

/** Pull the user-supplied reason text out of an event payload, falling back
 *  to whatever's on the parent decision when the event only carries a diff. */
export function describeReason(
  action: string,
  after: Record<string, unknown>,
  decision: Decision | null,
): string | null {
  if (action === "decide") {
    return (
      (after.actual_choice_reason as string | undefined) ??
      decision?.actual_choice_reason ??
      null
    );
  }
  if (action === "scope_out") {
    return (
      (after.scope_out_reason as string | undefined) ??
      (after.reason as string | undefined) ??
      decision?.out_of_scope_reason ??
      null
    );
  }
  return null;
}

/** Build a one-line plain-language summary for an audit event's payload.
 *  Uses the parent decision as a fallback context when the event payload
 *  doesn't carry recommendation fields directly. For relate/supersede,
 *  also looks up source/target summaries via decisionsById. */
export function describeEvent(
  ev: Event,
  decision: Decision | null,
  decisionsById: Map<string, Decision> = new Map(),
): string | null {
  const action = ev.action;
  const payload = (ev.payload ?? {}) as Record<string, unknown>;
  const after = (payload.after ?? {}) as Record<string, unknown>;

  if (action === "decide") {
    const choice =
      (after.actual_choice as string | undefined) ?? decision?.actual_choice;
    if (!choice) return null;

    const isRecAfter = after.is_recommended as boolean | undefined;
    const recAfter = after.recommended_summary as string | undefined;
    const recCurrent = decision?.recommended_summary;
    const recommended = recAfter ?? recCurrent;
    const recBy =
      (after.recommended_by as string | undefined) ?? decision?.recommended_by;

    const recExisted = Boolean(recommended);
    const followed =
      isRecAfter === true ||
      (recommended !== undefined && choice === recommended);

    if (followed) {
      return recBy
        ? `chose “${choice}” (followed recommendation from ${recBy})`
        : `chose “${choice}” (followed recommendation)`;
    }
    if (recExisted) {
      return `chose “${choice}” (overrode recommendation “${recommended}”)`;
    }
    return `chose “${choice}” (no recommendation existed)`;
  }

  if (action === "scope_out") {
    const reason =
      (after.scope_out_reason as string | undefined) ??
      (after.reason as string | undefined);
    return reason ? `reason: ${reason}` : null;
  }

  if (action === "supersede") {
    const newId =
      (payload.new as string | undefined) ??
      (after.superseded_by as string | undefined);
    const oldId = (payload.old as string | undefined) ?? ev.id;
    const newSummary = newId
      ? (decisionsById.get(newId)?.summary ?? newId.slice(0, 8))
      : null;
    const oldSummary =
      decisionsById.get(oldId)?.summary ??
      (after.summary as string | undefined) ??
      oldId.slice(0, 8);
    if (newSummary) return `“${oldSummary}” superseded by “${newSummary}”`;
    return `“${oldSummary}” superseded`;
  }

  if (action === "undecide") return "cleared the previous outcome";

  if (action === "create") {
    const summary = after.summary as string | undefined;
    return summary ? `“${summary}”` : null;
  }

  if (action === "relate" || action === "unrelate") {
    const srcId = (payload.src as string | undefined) ?? ev.id;
    const targetId = (payload.target as string | undefined) ?? "";
    const type = (payload.type as string | undefined) ?? "relates_to";
    const srcSummary =
      decisionsById.get(srcId)?.summary ?? srcId.slice(0, 8);
    const targetSummary =
      decisionsById.get(targetId)?.summary ?? targetId.slice(0, 8);
    const verb = type.replace(/_/g, " ");
    if (action === "unrelate") {
      return `removed ${verb}: “${srcSummary}” → “${targetSummary}”`;
    }
    return `“${srcSummary}” ${verb} “${targetSummary}”`;
  }

  if (action === "tree_create") {
    const title = (after.title as string | undefined) ?? ev.id;
    return title ? `“${title}”` : null;
  }
  if (action === "tree_rename") {
    const title = (after.title as string | undefined) ?? ev.id;
    return title ? `renamed to “${title}”` : null;
  }
  if (action === "tree_archive" || action === "tree_delete") {
    const title = (after.title as string | undefined) ?? ev.id;
    return title;
  }

  return null;
}
