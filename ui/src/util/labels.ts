// Display-friendly versions of enum values.

import type { Status, Priority } from "@/api/types.gen";

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

export function statusColor(s: Status | string): ChipColor {
  switch (s) {
    case "proposed": return "primary";
    case "decided": return "success";
    case "out_of_scope": return "default";
    case "superseded": return "warning";
    default: return "default";
  }
}
