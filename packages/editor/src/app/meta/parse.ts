import {
  emptyMeta,
  type EditorMeta,
  type FileMeta,
  type FlowMeta,
  type MockSpec,
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

/** Parse one reserved mock, or null when it names no block to stub. */
function parseMock(raw: unknown): MockSpec | null {
  if (!isRecord(raw)) return null;
  const block = str(raw.block);
  if (!block) return null;
  return { block, result: str(raw.result) };
}

/** Parse the reserved mocks list, keeping only well-formed entries. */
function parseMocks(raw: unknown): MockSpec[] | undefined {
  if (!Array.isArray(raw)) return undefined;
  const mocks = raw.map(parseMock).filter((m): m is MockSpec => m !== null);
  return mocks.length > 0 ? mocks : undefined;
}

function parseFlowMeta(raw: unknown): FlowMeta {
  if (!isRecord(raw)) return { inputs: [] };
  const inputs = Array.isArray(raw.inputs)
    ? raw.inputs.map(parseInput).filter((i): i is TestInput => i !== null)
    : [];
  const mocks = parseMocks(raw.mocks);
  return mocks ? { inputs, mocks } : { inputs };
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
