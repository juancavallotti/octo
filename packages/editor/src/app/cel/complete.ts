import { allCompletions, type CelEntry } from "./catalog";

/**
 * Pure completion logic for CEL, shared by the CEL field editor and (later) the
 * `{{ }}` template completion. It works purely on a string + caret offset, so it is
 * trivially unit-testable and carries no React/DOM state.
 *
 * This is the deliberately basic pass of issue #125: it completes the in-scope
 * variables and the catalogued functions by name prefix. It does NOT do
 * member/type-aware completion (`body.<field>`) — that is the metadata-driven
 * follow-up — so a member access after `.` yields no suggestions rather than
 * wrongly offering top-level names.
 */

const IDENT_CHAR = /[A-Za-z0-9_]/;

/** The identifier fragment the caret is completing, and where it sits. */
export interface CompletionQuery {
  /** The identifier text from its start up to the caret (may be empty). */
  token: string;
  /** Inclusive start / exclusive end (== caret) offsets of {@link token}. */
  range: [number, number];
  /** True when the fragment is a member access (the char before it is `.`). */
  member: boolean;
}

/** Read the identifier token immediately to the left of `caret`. */
export function tokenAt(text: string, caret: number): CompletionQuery {
  let start = caret;
  while (start > 0 && IDENT_CHAR.test(text[start - 1])) start--;
  const token = text.slice(start, caret);
  const member = start > 0 && text[start - 1] === ".";
  return { token, range: [start, caret], member };
}

/** The result of asking for completions at a caret. */
export interface CompletionResult {
  query: CompletionQuery;
  items: CelEntry[];
}

/**
 * Completions available at `caret` in `text`. While typing, an empty token yields
 * nothing (so the menu does not pop up on every keystroke); pass `explicit` (an
 * on-demand trigger like Ctrl+Space) to list everything. Member accesses after `.`
 * yield nothing in this basic pass.
 */
export function completionsAt(
  text: string,
  caret: number,
  opts?: { explicit?: boolean },
): CompletionResult {
  const query = tokenAt(text, caret);
  if (query.member) return { query, items: [] };
  if (query.token === "" && !opts?.explicit) return { query, items: [] };
  const lower = query.token.toLowerCase();
  const items = allCompletions().filter((e) =>
    e.name.toLowerCase().startsWith(lower),
  );
  return { query, items };
}

/**
 * Apply an accepted completion: replace the query range with the entry's name.
 * Functions get an opening paren and the caret placed inside; variables/other
 * leave the caret after the inserted name. Returns the new text and caret.
 */
export function applyCompletion(
  text: string,
  query: CompletionQuery,
  entry: CelEntry,
): { text: string; caret: number } {
  const [start, end] = query.range;
  const insert = entry.kind === "function" ? `${entry.name}(` : entry.name;
  const next = text.slice(0, start) + insert + text.slice(end);
  return { text: next, caret: start + insert.length };
}
