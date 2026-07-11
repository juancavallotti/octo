import {
  emptyMeta,
  type BlockMock,
  type EditorMeta,
  type FileMeta,
  type FlowMeta,
  type MockCase,
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
 * Unknown keys are preserved through a round-trip where they can be (a newer editor's
 * `mocks`, say), so an older editor does not silently strip what it doesn't understand.
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

function parseFlowMeta(raw: unknown): FlowMeta {
  if (!isRecord(raw)) return { inputs: [] };
  const inputs = Array.isArray(raw.inputs)
    ? raw.inputs.map(parseInput).filter((i): i is TestInput => i !== null)
    : [];
  const mocks = parseMocks(raw.mocks);
  const spies = parseSpies(raw.spies);
  return {
    inputs,
    ...(mocks ? { mocks } : {}),
    ...(spies ? { spies } : {}),
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
