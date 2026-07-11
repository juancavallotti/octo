/**
 * The editor's own bookkeeping about a project, stored beside the flows as
 * `.octo/editor-meta.json`. It is a *design-time* file, and being **undeclared** is
 * what keeps it harmless: a runtime only ever loads the resources its config declares,
 * so nothing asks for this one, and the Resources view hides it. Losing it costs you
 * saved test inputs, nothing more.
 *
 * The shape is keyed twice — by document, then by flow — so one file can describe a
 * whole workspace (the standalone app shares one file across a flows directory) while
 * the platform, whose store is already per-integration, simply has one entry.
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
 * Reserved. Mocks will let a block be stubbed out for a test run (so a flow calling a
 * payment API can be exercised without one). Nothing reads this yet; it is here so the
 * file format does not have to change when it lands.
 */
export interface MockSpec {
  /** The block to stub, as a breakpoint-style address. */
  block: string;
  /** The message the stub returns instead of running the block. */
  result?: string;
}

/** What the editor knows about one flow. */
export interface FlowMeta {
  inputs: TestInput[];
  /** Reserved; see {@link MockSpec}. */
  mocks?: MockSpec[];
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
