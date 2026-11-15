import type { Call } from "./types";

// The pending row shows a one-line preview that CSS cuts off, so a row-level
// approval is only offered when everything the approval covers is visible there.
// Anything else must be approved from the opened details.
export const MAX_ROW_ARGUMENTS = 400;

function hasLineBreak(v: unknown): boolean {
  if (typeof v === "string") return v.includes("\n") || v.includes("\r");
  if (Array.isArray(v)) return v.some(hasLineBreak);
  if (v && typeof v === "object") return Object.values(v).some(hasLineBreak);
  return false;
}

/** The reason a row-level approval is not offered, or "" when it is safe. */
export function quickApproveBlock(
  c: Pick<Call, "redactedSpans" | "argumentsObj">,
): string {
  if (c.redactedSpans)
    return `${c.redactedSpans} part${c.redactedSpans === 1 ? "" : "s"} of this call are hidden by redaction`;
  if (hasLineBreak(c.argumentsObj)) return "the arguments contain a line break";
  if (JSON.stringify(c.argumentsObj ?? {}).length > MAX_ROW_ARGUMENTS)
    return "the arguments are too long to read in one line";
  return "";
}
