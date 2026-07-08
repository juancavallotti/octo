/**
 * Bracket/quote auto-pairing for the code editors (CEL + JSON). A pure function over
 * (value, selection, key) so it is trivially testable and reused by both editors.
 *
 * Handled keys (only when there is no selection):
 *  - opener `(` `[` `{` — insert the matching closer, caret between them
 *  - quote `"` `'` — insert the matching quote, caret between (or skip over a closer)
 *  - closer `)` `]` `}` or quote already present under the caret — step over it
 *  - Backspace between an empty pair — delete both characters
 * Returns the new value + caret when it handled the key, or null to let the editor
 * insert the character normally.
 */

const OPENERS: Record<string, string> = { "(": ")", "[": "]", "{": "}" };
const CLOSERS = new Set([")", "]", "}"]);
const QUOTES = new Set(['"', "'"]);

export interface AutoPairResult {
  value: string;
  caret: number;
}

export function autoPair(
  value: string,
  start: number,
  end: number,
  key: string,
): AutoPairResult | null {
  const hasSelection = start !== end;

  // Backspace deletes both halves of an empty pair.
  if (key === "Backspace") {
    if (hasSelection || start === 0) return null;
    const prev = value[start - 1];
    const next = value[start];
    const emptyPair =
      (OPENERS[prev] && next === OPENERS[prev]) ||
      (QUOTES.has(prev) && next === prev);
    if (emptyPair) {
      return { value: value.slice(0, start - 1) + value.slice(start + 1), caret: start - 1 };
    }
    return null;
  }

  if (key.length !== 1 || hasSelection) return null;

  // Step over a closer/quote that is already the next character (typically one we
  // auto-inserted), instead of inserting a duplicate.
  if ((CLOSERS.has(key) || QUOTES.has(key)) && value[start] === key) {
    return { value, caret: start + 1 };
  }

  // Insert a matching closer for an opener or quote, caret between the pair.
  const closer = OPENERS[key] ?? (QUOTES.has(key) ? key : undefined);
  if (closer) {
    return {
      value: value.slice(0, start) + key + closer + value.slice(start),
      caret: start + 1,
    };
  }

  return null;
}
