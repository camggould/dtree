// Display-friendly versions of enum values.

import type { Status, Priority, Decision } from "@/api/types.gen";

/** Long-form decision body. The Go schema serialises this as
 *  `decision_full_description` for historical reasons; this helper
 *  centralises the lookup and falls back to plain `description` if
 *  a future API version normalises the name. */
export function decisionDescription(d: Decision): string {
  return d.decision_full_description ?? d.description ?? "";
}

/** Truncate text and append an ellipsis. Whitespace-aware. */
export function truncate(s: string, max: number): string {
  if (s.length <= max) return s;
  return s.slice(0, max).trimEnd() + "…";
}

export function humanStatus(s: Status | string): string {
  switch (s) {
    case "proposed": return "Proposed";
    case "decided": return "Decided";
    case "out_of_scope": return "Out of scope";
    case "superseded": return "Superseded";
    default: return String(s);
  }
}

export function humanPriority(p: Priority | string): string {
  switch (p) {
    case "assumption": return "Assumption";
    case "low": return "Low";
    case "medium": return "Medium";
    case "high": return "High";
    case "critical": return "Critical";
    default: return String(p);
  }
}

export type ChipColor =
  | "default"
  | "primary"
  | "secondary"
  | "success"
  | "warning"
  | "danger";

/** Display labels for audit-event Action enum values. */
export function humanAction(a: string): string {
  switch (a) {
    case "create": return "Created";
    case "update": return "Updated";
    case "delete": return "Deleted";
    case "decide": return "Decided";
    case "undecide": return "Undecided";
    case "scope_out": return "Marked out of scope";
    case "supersede": return "Superseded";
    case "restore": return "Restored";
    case "relate": return "Linked";
    case "unrelate": return "Unlinked";
    case "rename": return "Renamed";
    case "external_edit": return "Externally edited";
    case "external_create": return "Externally created";
    case "external_delete": return "Externally deleted";
    case "tree_create": return "Tree created";
    case "tree_delete": return "Tree deleted";
    case "tree_rename": return "Tree renamed";
    case "tree_archive": return "Tree archived";
    case "actor_add": return "Actor added";
    case "actor_rename": return "Actor renamed";
    case "actor_archive": return "Actor archived";
    case "config_change": return "Config changed";
    case "schema_migrate": return "Schema migrated";
    default: return a;
  }
}

export function statusColor(s: Status | string): ChipColor {
  switch (s) {
    case "proposed": return "primary";
    case "decided": return "success";
    case "out_of_scope": return "default";
    case "superseded": return "warning";
    default: return "default";
  }
}
