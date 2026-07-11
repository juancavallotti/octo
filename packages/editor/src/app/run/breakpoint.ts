import type { BlockNode, EditorDocument, FlowDoc } from "../model/document";

/**
 * Turns "break on this block" into something the runtime understands.
 *
 * `octo invoke --break-at` addresses a block by a path — `<flow>[<chain>].<block>[<branch>]…`
 * — where each block is selected by its name, else its type (see
 * runtime/core/runtime/breakpoint.go). The canvas, meanwhile, knows a block only by
 * its client id. This module bridges the two: given a document and a block id, it
 * derives the address the runner wants.
 *
 * The wrinkle is that the runtime's selector is not unique in general — two unnamed
 * `log` blocks in one chain both answer to "log", and the runner rejects the address
 * rather than guess. Naming a block just to debug it would be a miserable thing to
 * demand of a user, so we exploit the fact that **the YAML we invoke is generated and
 * thrown away**: this returns a *clone* of the document in which the blocks along the
 * path have been given synthetic names where their natural ones were unusable. The
 * saved document is never touched, and the user never has to name anything.
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
