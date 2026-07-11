import type { BlockNode, EditorDocument, FlowDoc } from "../model/document";

/**
 * Turns "this block, there on the canvas" into the address the runtime knows it by.
 *
 * `octo invoke`'s three debug features — `--break-at`, `--spies`, `--mocks` — all address
 * a block the same way: a path, `<flow>[<chain>].<block>[<branch>]…`, in which each block
 * is selected by its name, else its type (see runtime/core/runtime/address.go). The
 * canvas, meanwhile, knows a block only by its client id. This module bridges the two.
 *
 * ## Two kinds of address, and why there have to be two
 *
 * The runtime's selector is not unique in general: two unnamed `log` blocks in one chain
 * both answer to "log", and the runner rejects the address rather than guess. What to do
 * about that depends on how long the address has to live.
 *
 * **A breakpoint is thrown away with the run.** So {@link planBreakpoint} exploits the
 * fact that the YAML we invoke is generated and discarded: it returns a *clone* of the
 * document in which the blocks along the path carry synthetic names where their natural
 * ones would not do. The saved document is never touched, and the user never has to name
 * anything to run to a block.
 *
 * **A mock or a spy outlives the run**, in `.octo/editor-meta.json`, and has to be found
 * again on the next reload — when every client id is new. So it is keyed by
 * {@link naturalAddress}, which invents nothing: it returns null for a block whose label
 * would be ambiguous. Placing a mock or spy on such a block first gives it a real name
 * ({@link namesNeededFor}) — a visible edit to the document, and the price of an address
 * that still means something tomorrow.
 *
 * A synthetic name and a real one never collide over the same block: a block carrying a
 * mock or spy has been named, so its natural label is unique and `ensureAddressable`
 * leaves it exactly as it found it.
 */

/** Branch slots every composite spells the same way; the runtime names them identically. */
const RESERVED_SLOTS = new Set([
  "process",
  "error",
  "then",
  "else",
  "body",
  "default",
  "onReject",
]);

/** The prefix of a synthetic name. Deliberately unlikely to collide with a real one. */
const SYNTHETIC_PREFIX = "__bp_";

export interface BreakpointPlan {
  /**
   * A clone of the document to serialize for this invoke — identical to the original
   * except for any synthetic names needed to make the path addressable.
   */
  doc: EditorDocument;
  /** The root flow to invoke (a breakpoint is always reached by running its flow). */
  flow: string;
  /** The `--break-at` address. */
  address: string;
  /** The target block's display name, for the UI ("broke at …"). */
  label: string;
}

/** How a block is selected in an address: its name, else its type. */
function labelOf(block: BlockNode): string {
  return block.name?.trim() || block.type;
}

/**
 * Whether a string can stand as one segment of an address. The parser splits on `.`
 * and reads branches out of `[...]`, so a name carrying any of those characters would
 * be mis-parsed.
 */
function isCleanSegment(value: string): boolean {
  return value !== "" && !/[.[\]]/.test(value);
}

/** Whether a name would be read as a positional index rather than a name. */
function isIndexLike(value: string): boolean {
  return /^\d+$/.test(value);
}

/** A synthetic name no block in `chain` already answers to. */
function freshName(chain: BlockNode[]): string {
  const taken = new Set(chain.map(labelOf));
  let n = 1;
  while (taken.has(`${SYNTHETIC_PREFIX}${n}`)) n++;
  return `${SYNTHETIC_PREFIX}${n}`;
}

/**
 * The label that addresses `block` within `chain`, renaming it in place when its
 * natural label would not do: when a sibling answers to the same label (the runtime
 * refuses an ambiguous address), or when it carries a character the address parser
 * would choke on. Mutates the clone only.
 */
function ensureAddressable(chain: BlockNode[], block: BlockNode): string {
  const label = labelOf(block);
  const unique = chain.filter((b) => labelOf(b) === label).length === 1;
  if (unique && isCleanSegment(label)) return label;

  const name = freshName(chain);
  block.name = name;
  return name;
}

/** One block on the path to the target: where it lives, and which branch to descend. */
interface Step {
  /** The chain the block sits in — its siblings, for the uniqueness check. */
  chain: BlockNode[];
  block: BlockNode;
  /** Which branch of this composite to descend into. Absent on the target itself. */
  branch?: { slot: string; subs: FlowDoc[]; index: number };
}

/** The steps from `chain` down to the block with `blockId`, or null when it isn't here. */
function findSteps(chain: BlockNode[], blockId: string): Step[] | null {
  for (const block of chain) {
    if (block.id === blockId) return [{ chain, block }];
    if (!block.slots) continue;
    for (const [slot, subs] of Object.entries(block.slots)) {
      for (let index = 0; index < subs.length; index++) {
        const deeper = findSteps(subs[index].process, blockId);
        if (deeper) return [{ chain, block, branch: { slot, subs, index } }, ...deeper];
      }
    }
  }
  return null;
}

/**
 * The bracket token selecting one branch of a composite, or null when this branch
 * cannot be addressed unambiguously.
 *
 * The composites whose branches are a list (a fork's branches, a switch's cases, an
 * ai-router's routes, an ai-agent's tools) are addressed by the member's own name, or
 * by its index. We prefer the index whenever the name won't do — unlike a block, a
 * sub-flow's name is not ours to rewrite: an ai-router route and an ai-agent tool are
 * *chosen by the model* from their names, so renaming one would change what the flow
 * does.
 */
function branchToken(branch: NonNullable<Step["branch"]>): string | null {
  if (RESERVED_SLOTS.has(branch.slot)) return branch.slot;

  const { subs, index } = branch;
  const nameOf = (sub: FlowDoc) => sub.name?.trim() ?? "";
  const name = nameOf(subs[index]);

  const usable =
    isCleanSegment(name) &&
    !isIndexLike(name) && // would be read as a position, not a name
    subs.filter((s) => nameOf(s) === name).length === 1;
  if (usable) return name;

  // Fall back to the position. The runtime matches member names *before* indices, so
  // an index is only safe when no sibling is named that same integer — otherwise the
  // address would silently select the wrong branch. Refuse rather than mis-address.
  const position = String(index);
  if (subs.some((s) => nameOf(s) === position)) return null;
  return position;
}

/**
 * Derive the breakpoint address for a block, plus the document to invoke with it.
 * Returns null when the block isn't in the document, or in the rare case where its
 * branch cannot be addressed without ambiguity — callers hide the run-to-here
 * affordance rather than offer a run that would break on the wrong block.
 */
export function planBreakpoint(
  doc: EditorDocument,
  blockId: string,
): BreakpointPlan | null {
  const clone = structuredClone(doc);

  for (const flow of clone.flows) {
    // A flow's name is the address's first segment, and it is not ours to rewrite:
    // `flow-ref` blocks address flows by name, so renaming one would break the config
    // we are about to run.
    const chains: [chain: string, blocks: BlockNode[]][] = [
      ["process", flow.process],
      ["error", flow.error?.process ?? []],
    ];

    for (const [chainName, blocks] of chains) {
      const steps = findSteps(blocks, blockId);
      if (!steps) continue;
      if (!isCleanSegment(flow.name?.trim() ?? "")) return null;

      // The friendly label is read before ensureAddressable can overwrite the name.
      const target = steps[steps.length - 1].block;
      const label = labelOf(target);

      const head = chainName === "error" ? `${flow.name}[error]` : flow.name;
      const segments = [head];

      for (const step of steps) {
        const selector = ensureAddressable(step.chain, step.block);
        if (!step.branch) {
          segments.push(selector);
          continue;
        }
        const token = branchToken(step.branch);
        if (token === null) return null;
        segments.push(`${selector}[${token}]`);
      }

      return { doc: clone, flow: flow.name, address: segments.join("."), label };
    }
  }

  return null;
}

// ---------------------------------------------------------------------------
// Durable addresses: what a mock or a spy is keyed by.
// ---------------------------------------------------------------------------

/** Where a block sits: the flow rooting its address, and the path down to it. */
interface Located {
  flow: FlowDoc;
  /** "process" or "error" — the flow chain the block hangs off. */
  chain: string;
  steps: Step[];
}

/** Find the block in the document, or null when it isn't there. */
function locate(doc: EditorDocument, blockId: string): Located | null {
  for (const flow of doc.flows) {
    const chains: [chain: string, blocks: BlockNode[]][] = [
      ["process", flow.process],
      ["error", flow.error?.process ?? []],
    ];
    for (const [chain, blocks] of chains) {
      const steps = findSteps(blocks, blockId);
      if (steps) return { flow, chain, steps };
    }
  }
  return null;
}

/** Whether `block`'s own label addresses it unambiguously within `chain`. */
function isAddressable(chain: BlockNode[], block: BlockNode): boolean {
  const label = labelOf(block);
  return isCleanSegment(label) && chain.filter((b) => labelOf(b) === label).length === 1;
}

/**
 * The address a block answers to as the document stands — inventing nothing.
 *
 * Null when any block on the path is ambiguous (a sibling answers to the same label) or
 * carries a character the address parser would choke on, and null when the branch or the
 * flow name cannot be addressed at all. This is what a mock or a spy is keyed by, so it
 * must be derivable from the saved document alone: a key that depended on a name we
 * invented at run time would not survive the reload it exists to survive.
 */
export function naturalAddress(doc: EditorDocument, blockId: string): string | null {
  const found = locate(doc, blockId);
  if (!found) return null;

  const flowName = found.flow.name?.trim() ?? "";
  if (!isCleanSegment(flowName)) return null;

  const segments = [found.chain === "error" ? `${flowName}[error]` : flowName];
  for (const step of found.steps) {
    if (!isAddressable(step.chain, step.block)) return null;
    const selector = labelOf(step.block);
    if (!step.branch) {
      segments.push(selector);
      continue;
    }
    const token = branchToken(step.branch);
    if (token === null) return null;
    segments.push(`${selector}[${token}]`);
  }
  return segments.join(".");
}

/** Every block's natural address, by client id — absent when it has none. */
export function blockIdAddresses(doc: EditorDocument): Map<string, string> {
  const out = new Map<string, string>();
  const walk = (blocks: BlockNode[]) => {
    for (const block of blocks) {
      const address = naturalAddress(doc, block.id);
      if (address) out.set(block.id, address);
      for (const subs of Object.values(block.slots ?? {})) {
        for (const sub of subs) walk(sub.process);
      }
    }
  };
  for (const flow of doc.flows) {
    walk(flow.process);
    walk(flow.error?.process ?? []);
  }
  return out;
}

/** A real name to give a block so it can be addressed. */
export interface BlockRename {
  blockId: string;
  name: string;
}

/**
 * A label no sibling in `chain` already answers to, built from the block's type so the
 * name the user ends up seeing reads like one they might have written: `log`, else
 * `log-2`, `log-3`. Unlike a breakpoint's synthetic name this one is *saved*, so it has
 * to be presentable.
 */
function freshLabel(chain: BlockNode[], block: BlockNode): string {
  const taken = new Set(chain.map(labelOf));
  if (isCleanSegment(block.type) && !taken.has(block.type)) return block.type;
  const base = isCleanSegment(block.type) ? block.type : "block";
  let n = 2;
  while (taken.has(`${base}-${n}`)) n++;
  return `${base}-${n}`;
}

/**
 * The renames that would give a block a durable address — the price of putting a mock or
 * a spy on it.
 *
 * An empty list means it already has one: place the mock and touch nothing. A non-empty
 * list is a real edit to the saved document, which the caller applies before reading the
 * address back; it can span several blocks, because an ambiguous *composite* on the path
 * makes everything under it ambiguous too.
 *
 * Null means no naming can help — the branch or the flow name is the problem, not the
 * block's label — and the caller should not offer the affordance at all, exactly as
 * run-to-here already hides itself in that case.
 */
export function namesNeededFor(doc: EditorDocument, blockId: string): BlockRename[] | null {
  const found = locate(doc, blockId);
  if (!found) return null;
  if (!isCleanSegment(found.flow.name?.trim() ?? "")) return null;

  // Each step descends into a different chain — the root's, then a sub-flow's — so no two
  // renames here ever compete for a name in the same chain.
  const renames: BlockRename[] = [];
  for (const step of found.steps) {
    if (step.branch && branchToken(step.branch) === null) return null;
    if (isAddressable(step.chain, step.block)) continue;
    renames.push({ blockId: step.block.id, name: freshLabel(step.chain, step.block) });
  }
  return renames;
}

// ---------------------------------------------------------------------------
// Conflicts: what a mock makes unreachable.
// ---------------------------------------------------------------------------

/**
 * Whether `inner` addresses a block inside the block `outer` addresses. Mirrors
 * `isInside` in runtime/core/runtime/mock.go.
 *
 * The separator matters: `orders.fan` is not inside `orders.fanout`, and only a `.` or a
 * `[` can follow a complete address segment.
 */
function isInside(outer: string, inner: string): boolean {
  return inner.startsWith(`${outer}.`) || inner.startsWith(`${outer}[`);
}

/**
 * The reason a run cannot be made, or null when it can.
 *
 * A mock *replaces* the block it stands in for, deleting whatever was inside it — so a
 * breakpoint or a spy addressed in there can never fire, and the runtime rejects the whole
 * run rather than report nothing. Its own error would say `no block "log-it" in that
 * chain`, which reads like a typo when the truth is that the user mocked away the very
 * thing they were trying to watch. Catch it here and say that instead.
 */
export function debugConflict(mocks: string[], observers: string[]): string | null {
  for (const mock of mocks) {
    for (const observer of observers) {
      if (isInside(mock, observer)) {
        return `"${observer}" is inside the mocked block "${mock}", so it can never run — a mock replaces the block it stands in for. Remove the mock, or move what you are watching outside it.`;
      }
    }
  }
  return null;
}
