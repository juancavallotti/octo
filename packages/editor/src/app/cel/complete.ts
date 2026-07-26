import {
  allCompletions,
  LIST_METHODS,
  MAP_METHODS,
  namespaceMembers,
  STRING_METHODS,
  type CelEntry,
} from "./catalog";

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

/**
 * Whether `caret` sits inside an unclosed string literal. Scans from the start,
 * tracking the active quote and honoring backslash escapes, so completions are
 * suppressed while typing string contents (e.g. `body.startsWith("t|`).
 */
export function isInsideString(text: string, caret: number): boolean {
  let quote = "";
  for (let i = 0; i < caret; i++) {
    const ch = text[i];
    if (quote) {
      if (ch === "\\") i++; // skip the escaped character
      else if (ch === quote) quote = "";
    } else if (ch === '"' || ch === "'") {
      quote = ch;
    }
  }
  return quote !== "";
}

/** Read the identifier token immediately to the left of `caret`. */
export function tokenAt(text: string, caret: number): CompletionQuery {
  let start = caret;
  while (start > 0 && IDENT_CHAR.test(text[start - 1])) start--;
  const token = text.slice(start, caret);
  const member = start > 0 && text[start - 1] === ".";
  return { token, range: [start, caret], member };
}

/**
 * Supplies member completions for a dotted path, e.g. `["body", "user"]` → the keys
 * of `body.user`. Returns undefined when the path can't be resolved (so the caller
 * offers nothing rather than guessing). Used by the CEL tester, which resolves paths
 * against the body/vars/env JSON samples.
 */
export type MemberProvider = (path: string[]) => CelEntry[] | undefined;

/**
 * The dotted base path and token the caret is completing as a member access, or null
 * when the caret is not a simple `a.b.c.<token>` member. Bails on non-identifier
 * bases (e.g. `foo().` or `x["k"].`), which this basic pass doesn't resolve.
 */
export function memberContext(
  text: string,
  caret: number,
): { base: string[]; token: string; range: [number, number] } | null {
  let ts = caret;
  while (ts > 0 && IDENT_CHAR.test(text[ts - 1])) ts--;
  const token = text.slice(ts, caret);
  if (!(ts > 0 && text[ts - 1] === ".")) return null;
  const base: string[] = [];
  let i = ts - 1; // index of the '.'
  while (text[i] === ".") {
    let s = i;
    while (s > 0 && IDENT_CHAR.test(text[s - 1])) s--;
    if (s === i) return null; // nothing but a dot before it — unsupported base
    base.unshift(text.slice(s, i));
    i = s - 1;
  }
  return base.length ? { base, token, range: [ts, caret] } : null;
}

/**
 * The value kind a member access is called on, inferred cheaply from the token/char
 * just before the dot: a `]` means a list literal, a closing quote means a string
 * literal. Used to offer receiver-style methods (list.map, string.startsWith, …)
 * even without a data sample.
 */
export function literalReceiver(
  text: string,
  dotIndex: number,
): "list" | "map" | "string" | null {
  let j = dotIndex - 1;
  while (j >= 0 && /\s/.test(text[j])) j--;
  const ch = text[j];
  if (ch === "]") return "list";
  if (ch === "}") return "map";
  if (ch === '"' || ch === "'") return "string";
  return null;
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
  opts?: { explicit?: boolean; members?: MemberProvider },
): CompletionResult {
  const query = tokenAt(text, caret);
  // No completions inside string contents.
  if (isInsideString(text, caret)) return { query, items: [] };
  // Member access after `.`.
  if (query.member) {
    const lower = query.token.toLowerCase();
    const byPrefix = (list: CelEntry[]) =>
      list.filter((e) => e.name.toLowerCase().startsWith(lower));
    // 1) A list/map/string literal receiver (`[...]. ` / `{...}. ` / `"...". `).
    const lit = literalReceiver(text, query.range[0] - 1);
    if (lit === "list") return { query, items: byPrefix(LIST_METHODS) };
    if (lit === "map") return { query, items: byPrefix(MAP_METHODS) };
    if (lit === "string") return { query, items: byPrefix(STRING_METHODS) };
    // 2) A namespaced function family (`math.`, `regex.`, …). These are globals,
    //    not member calls, so they resolve from the catalogue rather than the data
    //    — and they win over the provider, which cannot know these names.
    const path = memberContext(text, caret);
    const ns = path?.base.length === 1 ? namespaceMembers(path.base[0]) : undefined;
    if (ns) return { query, items: byPrefix(ns) };
    // 3) An identifier path resolved by the provider (e.g. JSON samples). The
    //    provider decides what a resolved list/string/object offers.
    const entries = path && opts?.members ? opts.members(path.base) : undefined;
    if (!entries) return { query, items: [] };
    return { query, items: byPrefix(entries) };
  }
  if (query.token === "" && !opts?.explicit) return { query, items: [] };
  const lower = query.token.toLowerCase();
  const items = allCompletions().filter((e) =>
    e.name.toLowerCase().startsWith(lower),
  );
  return { query, items };
}

/** What accepting an entry inserts: a call opens its paren, a namespace its dot. */
function insertionFor(entry: CelEntry): string {
  if (entry.kind === "function") return `${entry.name}(`;
  if (entry.kind === "namespace") return `${entry.name}.`;
  return entry.name;
}

/**
 * Apply an accepted completion: replace the query range with the entry's name.
 * Functions get an opening paren and the caret placed inside, a namespace gets the
 * dot that offers its members next; variables/other leave the caret after the
 * inserted name. Returns the new text and caret.
 */
export function applyCompletion(
  text: string,
  query: CompletionQuery,
  entry: CelEntry,
): { text: string; caret: number } {
  const [start, end] = query.range;
  const insert = insertionFor(entry);
  const next = text.slice(0, start) + insert + text.slice(end);
  return { text: next, caret: start + insert.length };
}
