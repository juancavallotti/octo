/**
 * Whether a suite file carries comments.
 *
 * The form writes back by re-serializing the model, and the model has nowhere to put a
 * comment — so every one of them is dropped the first time somebody edits through the
 * form. These files are committed and heavily commented (the scaffold a new suite starts
 * from is mostly comment), which makes that a real loss, and one the user cannot see
 * happening.
 *
 * So the tab says so, where it can still be acted on. Detecting it rather than warning
 * unconditionally matters: a warning that is always there is one nobody reads, and a
 * suite written entirely through the form has nothing left to lose.
 */

import YAML from "yaml";

/** True if the document has a comment anywhere in it. */
export function hasComments(content: string): boolean {
  let found = false;
  try {
    const doc = YAML.parseDocument(content);
    // A file that is nothing but comments parses to no contents at all, and they hang
    // off the document rather than off any node.
    if (doc.comment || doc.commentBefore) return true;
    YAML.visit(doc, (_key, node) => {
      const commented = node as { comment?: string | null; commentBefore?: string | null };
      if (commented?.comment || commented?.commentBefore) {
        found = true;
        return YAML.visit.BREAK;
      }
    });
  } catch {
    // Unreadable YAML is the YAML view's problem, not this one's; the form is disabled
    // for it anyway (parse.ts reports it as lossy).
    return false;
  }
  return found;
}
