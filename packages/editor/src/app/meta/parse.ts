import {
  emptyMeta,
  type BlockMock,
  type EditorMeta,
  type FileMeta,
  type EncodedShape,
  type FlowMeta,
  type MockCase,
  type ObservedEntry,
  type ObservedMessage,
  type TestInput,
} from "./types";

/**
 * Reading and writing `.octo/editor-meta.json`.
 *
 * Parsing is deliberately lenient: this file is a convenience, not a source of truth,
 * and it sits in a directory users can hand-edit and commit. Anything unreadable —
 * absent, malformed JSON, the wrong shape, a hand-mangled entry — degrades to "no
 * saved inputs" for the part that is broken, rather than throwing and taking the
 * editor down with it. The cost of being wrong here is a lost test input; the cost of
 * throwing is a blank screen.
 *
 * Unknown keys are NOT preserved through a round-trip: parseFlowMeta returns only the
 * fields it knows, and serialize re-emits what it parsed. An older editor opening a
 * project therefore drops a newer one's additions. That is tolerable for what this
 * file holds — `observed` is a cache that regenerates, and the rest is scratch — but
 * it is the constraint to check against before putting anything here that a user
 * could not reproduce.
 */

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/** A string, or undefined when the value is anything else. */
function str(value: unknown): string | undefined {
  return typeof value === "string" ? value : undefined;
}

/** Parse one saved input, or null when it carries no usable identity. */
function parseInput(raw: unknown): TestInput | null {
  if (!isRecord(raw)) return null;
  const id = str(raw.id);
  const name = str(raw.name);
  if (!id || !name) return null; // an input we cannot address or label is not one
  return { id, name, data: str(raw.data), vars: str(raw.vars) };
}

/**
 * Parse one mock case. Every field is optional here even though the runtime demands
 * exactly one outcome, because this is a file a user can hand-edit: a case that says
 * nothing is dropped on the way to the runner (see run/debug.ts), not rejected on the way
 * in, so one malformed case does not cost the user the rest of the mock.
 */
function parseCase(raw: unknown): MockCase | null {
  if (!isRecord(raw)) return null;
  const drop = raw.drop === true;
  const parsed: MockCase = {
    when: str(raw.when),
    body: str(raw.body),
    vars: str(raw.vars),
    error: str(raw.error),
    ...(drop ? { drop: true } : {}),
  };
  return parsed;
}

/** Parse one mocked block, or null when it names no block to stand in for. */
function parseMock(raw: unknown): BlockMock | null {
  if (!isRecord(raw)) return null;
  const address = str(raw.address);
  if (!address) return null; // a mock we cannot address is not one
  const cases = Array.isArray(raw.cases)
    ? raw.cases.map(parseCase).filter((c): c is MockCase => c !== null)
    : [];
  const fallback = parseCase(raw.default);
  return {
    address,
    // An entry written without `enabled` is one that means to apply: a mock is saved
    // because the user wants it, and the toggle is how they say otherwise.
    enabled: raw.enabled !== false,
    cases,
    ...(fallback ? { default: fallback } : {}),
  };
}

/** Parse the mocks list, keeping only well-formed entries. */
function parseMocks(raw: unknown): BlockMock[] | undefined {
  if (!Array.isArray(raw)) return undefined;
  const mocks = raw.map(parseMock).filter((m): m is BlockMock => m !== null);
  return mocks.length > 0 ? mocks : undefined;
}

/** Parse the spied addresses, dropping anything that is not one. */
function parseSpies(raw: unknown): string[] | undefined {
  if (!Array.isArray(raw)) return undefined;
  const spies = [...new Set(raw.filter((s): s is string => typeof s === "string" && s !== ""))];
  return spies.length > 0 ? spies : undefined;
}

/** How deep a stored shape is read, matching the cap the producer writes under. */
const MAX_SHAPE_DEPTH = 5;

/**
 * Read a stored shape, degrading rather than throwing.
 *
 * The depth cap is enforced on READ as well as on write: this file is hand-editable,
 * and a shape nested a thousand deep would otherwise recurse until the stack gave
 * out — in the parser, where every consumer of the file would meet it.
 */
function parseShape(raw: unknown, depth = 0): EncodedShape | undefined {
  if (!isRecord(raw) || depth >= MAX_SHAPE_DEPTH) return undefined;
  const t = raw.t;
  if (typeof t !== "string") return undefined;
  if (!["string", "number", "bool", "null", "list", "object", "dyn"].includes(t)) {
    // A tag from a newer editor: it exists, and we do not know its shape.
    return { t: "dyn" };
  }
  if (t === "list") {
    const of = parseShape(raw.of, depth + 1);
    return of ? { t: "list", of } : { t: "list" };
  }
  if (t === "object") {
    // No `f` at all means a map whose contents were deliberately not recorded, and
    // that has to survive the round trip: rebuilding it as `f: {}` would turn it into
    // an empty object, which the merger is entitled to union keys into.
    if (!isRecord(raw.f)) return { t: "object" };
    const f: Record<string, EncodedShape> = {};
    for (const [key, value] of Object.entries(raw.f)) {
      const shape = parseShape(value, depth + 1);
      if (shape) f[key] = shape;
    }
    return { t: "object", f };
  }
  return { t: t as EncodedShape["t"] };
}

function parseObservedMessage(raw: unknown): ObservedMessage | undefined {
  if (!isRecord(raw)) return undefined;
  const body = parseShape(raw.body);
  const vars = parseShape(raw.vars);
  return body || vars ? { ...(body ? { body } : {}), ...(vars ? { vars } : {}) } : undefined;
}

/** Observed shapes by block address, keeping only the entries that say something. */
function parseObserved(raw: unknown): Record<string, ObservedEntry> | undefined {
  if (!isRecord(raw)) return undefined;
  const out: Record<string, ObservedEntry> = {};
  for (const [address, value] of Object.entries(raw)) {
    if (!isRecord(value)) continue;
    const received = parseObservedMessage(value.in);
    const produced = parseObservedMessage(value.out);
    if (received || produced) {
      out[address] = { ...(received ? { in: received } : {}), ...(produced ? { out: produced } : {}) };
    }
  }
  return Object.keys(out).length > 0 ? out : undefined;
}

function parseFlowMeta(raw: unknown): FlowMeta {
  if (!isRecord(raw)) return { inputs: [] };
  const inputs = Array.isArray(raw.inputs)
    ? raw.inputs.map(parseInput).filter((i): i is TestInput => i !== null)
    : [];
  const mocks = parseMocks(raw.mocks);
  const spies = parseSpies(raw.spies);
  const observed = parseObserved(raw.observed);
  return {
    inputs,
    ...(mocks ? { mocks } : {}),
    ...(spies ? { spies } : {}),
    ...(observed ? { observed } : {}),
  };
}

function parseFileMeta(raw: unknown): FileMeta {
  if (!isRecord(raw) || !isRecord(raw.flows)) return { flows: {} };
  const flows: Record<string, FlowMeta> = {};
  for (const [name, value] of Object.entries(raw.flows)) {
    flows[name] = parseFlowMeta(value);
  }
  return { flows };
}

/**
 * Parse the file's content. Empty or unreadable content yields an empty meta — the
 * editor then behaves exactly as it does for a project that has never saved an input.
 */
export function parseEditorMeta(content: string): EditorMeta {
  if (content.trim() === "") return emptyMeta();

  let raw: unknown;
  try {
    raw = JSON.parse(content);
  } catch {
    return emptyMeta(); // hand-mangled JSON: start over rather than fail
  }
  if (!isRecord(raw) || !isRecord(raw.resources)) return emptyMeta();

  const resources: Record<string, FileMeta> = {};
  for (const [key, value] of Object.entries(raw.resources)) {
    resources[key] = parseFileMeta(value);
  }
  return { version: 1, resources };
}

/**
 * Serialize the meta for storage. Keys are sorted and the output is indented so the
 * file diffs cleanly — it lives beside the flows and people will commit it.
 */
export function serializeEditorMeta(meta: EditorMeta): string {
  const resources: Record<string, FileMeta> = {};
  for (const key of Object.keys(meta.resources).sort()) {
    const file = meta.resources[key];
    const flows: Record<string, FlowMeta> = {};
    for (const name of Object.keys(file.flows).sort()) {
      flows[name] = file.flows[name];
    }
    resources[key] = { flows };
  }
  return JSON.stringify({ version: 1, resources }, null, 2) + "\n";
}

/** The entry for one document, or an empty one when it has none yet. */
export function fileMetaFor(meta: EditorMeta, documentKey: string): FileMeta {
  return meta.resources[documentKey] ?? { flows: {} };
}

/** Return a copy of `meta` with `file` stored under `documentKey`. */
export function withFileMeta(
  meta: EditorMeta,
  documentKey: string,
  file: FileMeta,
): EditorMeta {
  return { ...meta, resources: { ...meta.resources, [documentKey]: file } };
}
