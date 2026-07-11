/**
 * The editor's own bookkeeping about a project, stored beside the flows as
 * `.octo/editor-meta.json`. It is a *design-time* file, and being **undeclared** is
 * what keeps it harmless: a runtime only ever loads the resources its config declares,
 * so nothing asks for this one, and the Resources view hides it. Losing it costs you the
 * test inputs, mocks and spies you saved — nothing the flows themselves depend on.
 *
 * The shape is keyed twice — by document, then by flow — so one file can describe a
 * whole workspace (the standalone app shares one file across a flows directory) while
 * the platform, whose store is already per-integration, simply has one entry.
 *
 * Within a flow, test inputs are addressed by an id of our own minting, while mocks and
 * spies are addressed by the *runtime's* block address. That asymmetry is forced: an
 * input is ours alone, but a mock and a spy have to name a block to the runner, and the
 * only name the runner knows is the address.
 */

/** A saved input to run a flow with — the body and variables of a test message. */
export interface TestInput {
  /** Stable id, so a rename of `name` doesn't lose which input was last used. */
  id: string;
  name: string;
  /** JSON request body, bound to the message's `body`. */
  data?: string;
  /**
   * JSON object seeding the message's variables. Sources are not started for an
   * invoke, so nothing sets these for you — this is where the headers an HTTP source
   * would have copied across come from.
   */
  vars?: string;
}

/**
 * One canned outcome of a mocked block: a CEL condition, and what the block should do
 * when it holds. Mirrors the runtime's `core.MockCase` (runtime/core/mock.go).
 *
 * `body` and `vars` are JSON *strings* here, not values, exactly as {@link TestInput}'s
 * are: the form edits text and validates that it parses, and the text is what survives a
 * half-finished edit. They are parsed on the way to the runner.
 *
 * The runtime enforces two rules this type cannot express, and {@link isValidCase} is
 * where the editor holds itself to them: exactly one of `body`/`error`/`drop` is set, and
 * `vars` only goes alongside a `body`.
 */
export interface MockCase {
  /** CEL, evaluated against the message the block RECEIVED. Absent only on a default. */
  when?: string;
  /** JSON body the block returns instead of running. A literal, not an expression. */
  body?: string;
  /** JSON object of variables set alongside `body`. */
  vars?: string;
  /** Fail the block with this message — how an error path is exercised. */
  error?: string;
  /** Filter the message out, as a filter block would. */
  drop?: boolean;
}

/**
 * A block stood in for on every run, so the real one never executes: the way to exercise
 * a flow whose blocks call a payment API or an LLM.
 *
 * Keyed by `address` — the runtime's block path — and NOT by the editor's block id, which
 * is minted fresh on every parse and so cannot survive a reload. See run/address.ts: a
 * block whose address would be ambiguous is given a real name when a mock is placed on it,
 * so that this key means something tomorrow.
 *
 * `enabled` is what the button toggles. A disabled mock is kept rather than deleted, so
 * turning mocking off for one run does not cost the user the spec they wrote.
 */
export interface BlockMock {
  address: string;
  enabled: boolean;
  /** Tried in order; the first whose `when` holds wins. */
  cases: MockCase[];
  /**
   * What the block does when no case matched. Without one, an unmatched message FAILS the
   * block — the mock replaced it, so there is no real block to fall through to.
   */
  default?: MockCase;
}

/** Whether a case sets exactly one outcome, as the runtime demands. */
export function isValidCase(c: MockCase): boolean {
  const outcomes = [c.body !== undefined, c.error !== undefined && c.error !== "", c.drop === true];
  if (outcomes.filter(Boolean).length !== 1) return false;
  // A block that failed or dropped returned no message, so it set no variables either.
  return !(c.vars !== undefined && c.body === undefined);
}

/** What the editor knows about one flow. */
export interface FlowMeta {
  inputs: TestInput[];
  /** Blocks stood in for, by address. */
  mocks?: BlockMock[];
  /**
   * Addresses of the blocks with a spy on them: every message crossing one is recorded
   * and reported back. A plain list, because a spy has nothing to configure — it is on
   * or it is not.
   */
  spies?: string[];
}

/**
 * What the editor knows about one document, keyed by FLOW NAME — not by the client id,
 * which is minted fresh on every parse and so cannot survive a reload.
 */
export interface FileMeta {
  flows: Record<string, FlowMeta>;
}

/** The file: documents keyed by the id their store addresses them by. */
export interface EditorMeta {
  version: 1;
  resources: Record<string, FileMeta>;
}

/** An empty, valid meta document. */
export function emptyMeta(): EditorMeta {
  return { version: 1, resources: {} };
}

/** An empty, valid entry for one flow. */
export function emptyFlowMeta(): FlowMeta {
  return { inputs: [] };
}
