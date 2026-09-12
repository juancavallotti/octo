import { shapeAtPath } from "./shape";
import type { Scope, ValueShape } from "./types";

/**
 * Reading a shape out of a CEL expression, without evaluating it.
 *
 * Two cases carry almost all of the value, and both are extremely common in real
 * flows. A `set-payload` whose value is a map literal —
 * `{"orderId": "A-100", "type": "physical"}` — states the body's keys outright. And a
 * `set-variable` whose value is a path — `vars.incident.watchName` — has whatever
 * that path already had.
 *
 * Everything else returns undefined. This is deliberately not a CEL parser: a
 * ternary, a function call, a concatenation are all "we do not know", and saying so
 * is cheaper and more honest than a parser that is wrong in ways nobody can predict.
 */

/** A cursor over the expression, so the readers below can stay small. */
interface Cursor {
  text: string;
  at: number;
}

function skipSpace(c: Cursor): void {
  while (c.at < c.text.length && /\s/.test(c.text[c.at])) c.at++;
}

/** Read a quoted string, returning its contents, or null when there is not one. */
function readString(c: Cursor): string | null {
  const quote = c.text[c.at];
  if (quote !== '"' && quote !== "'") return null;
  let out = "";
  let i = c.at + 1;
  while (i < c.text.length) {
    const ch = c.text[i];
    if (ch === "\\") {
      // Escapes are passed through as the escaped character. Key names with escapes
      // are vanishingly rare, and getting the exact CEL escape table right matters
      // far less than not mis-terminating the string.
      out += c.text[i + 1] ?? "";
      i += 2;
      continue;
    }
    if (ch === quote) {
      c.at = i + 1;
      return out;
    }
    out += ch;
    i++;
  }
  return null; // unterminated — still being typed
}

const IDENT = /[A-Za-z0-9_]/;

/**
 * Skip one value without describing it, so a map's later keys survive a value this
 * reader cannot type. Returns false when the expression is malformed from here.
 */
function skipValue(c: Cursor): boolean {
  skipSpace(c);
  let depth = 0;
  while (c.at < c.text.length) {
    const ch = c.text[c.at];
    if (ch === '"' || ch === "'") {
      if (readString(c) === null) return false;
      continue;
    }
    if (ch === "{" || ch === "[" || ch === "(") depth++;
    if (ch === "}" || ch === "]" || ch === ")") {
      if (depth === 0) return true; // the enclosing collection's closer
      depth--;
    }
    if (ch === "," && depth === 0) return true;
    c.at++;
  }
  return true;
}

function readValue(c: Cursor, depth: number): ValueShape {
  skipSpace(c);
  const start = c.at;
  const ch = c.text[c.at];

  if (ch === '"' || ch === "'") {
    return readString(c) === null ? { kind: "unknown" } : { kind: "string" };
  }
  if (ch === "{") {
    const map = readMap(c, depth + 1);
    if (map) return map;
    c.at = start;
    return skipValue(c) ? { kind: "dyn" } : { kind: "unknown" };
  }
  if (ch === "[") {
    const list = readList(c, depth + 1);
    if (list) return list;
    c.at = start;
    return skipValue(c) ? { kind: "dyn" } : { kind: "unknown" };
  }

  // A bare word or number, up to the next separator.
  let end = c.at;
  while (end < c.text.length && IDENT.test(c.text[end])) end++;
  const word = c.text.slice(c.at, end);
  const after = c.text[end];
  // A word followed by anything that continues an expression (a call, a dot, an
  // operator) is not a literal — skip it and admit we do not know.
  const literal = after === undefined || /[\s,}\]]/.test(after);
  if (literal && word !== "") {
    c.at = end;
    if (word === "true" || word === "false") return { kind: "bool" };
    if (word === "null") return { kind: "null" };
    if (/^\d+(\.\d+)?u?$/.test(word)) return { kind: "number" };
    return { kind: "dyn" };
  }
  c.at = start;
  return skipValue(c) ? { kind: "dyn" } : { kind: "unknown" };
}

/** How deep a literal is read. Past this it stops describing and says `dyn`. */
const MAX_DEPTH = 6;

function readList(c: Cursor, depth: number): ValueShape | null {
  if (c.text[c.at] !== "[" || depth > MAX_DEPTH) return null;
  c.at++;
  skipSpace(c);
  if (c.text[c.at] === "]") {
    c.at++;
    return { kind: "list", of: { kind: "unknown" } };
  }
  const of = readValue(c, depth);
  // Only the first element types the list; merging the rest would be more precise
  // and is not worth a second pass over a literal nobody writes heterogeneously.
  while (c.at < c.text.length && c.text[c.at] !== "]") c.at++;
  if (c.text[c.at] !== "]") return null;
  c.at++;
  return { kind: "list", of };
}

function readMap(c: Cursor, depth = 0): ValueShape | null {
  skipSpace(c);
  if (c.text[c.at] !== "{" || depth > MAX_DEPTH) return null;
  c.at++;

  const fields: Record<string, { shape: ValueShape; origin: "inferred"; certain: true }> = {};
  for (;;) {
    skipSpace(c);
    if (c.text[c.at] === "}") {
      c.at++;
      return { kind: "object", fields, open: false };
    }
    if (c.at >= c.text.length) return null; // unterminated

    const key = readString(c);
    // A key that is not a string literal means this is a computed map — the keys are
    // data, and naming them would be inventing them.
    if (key === null) return null;
    skipSpace(c);
    if (c.text[c.at] !== ":") return null;
    c.at++;

    fields[key] = { shape: readValue(c, depth), origin: "inferred", certain: true };
    skipSpace(c);
    if (c.text[c.at] === ",") c.at++;
    else if (c.text[c.at] !== "}") return null;
  }
}

/** The dotted path an expression is, or null when it is anything more than one. */
export function pathOf(expression: string): string[] | null {
  const text = expression.trim();
  if (!/^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$/.test(text)) return null;
  return text.split(".");
}

/**
 * What an expression evaluates to, as far as it can be read.
 *
 * A map literal is described exactly — its keys are closed, because a literal really
 * does say what is in it. A path is whatever that path already holds in `scope`.
 * Anything else is undefined: not knowing is a fine answer, and the caller treats it
 * as one.
 */
export function shapeOfExpression(expression: string, scope?: Scope): ValueShape | undefined {
  const text = expression.trim();
  if (text === "") return undefined;

  const literal = readMap({ text, at: 0 });
  if (literal) return literal;

  const list = readList({ text, at: 0 }, 0);
  if (list) return list;

  const path = pathOf(text);
  if (path && scope) {
    const [head, ...rest] = path;
    const root = scope.roots[head];
    if (root) return shapeAtPath(root.shape, rest);
  }
  return undefined;
}
