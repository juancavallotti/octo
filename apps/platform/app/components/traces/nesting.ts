/**
 * Deciding what ran inside what.
 *
 * Two sources of evidence, and they are not equal. A block's **address** says
 * structurally where it sits — `orders.fan[0].call` ran inside `orders.fan`, and
 * nothing about a clock can change that. The **clock** is what links things the
 * address cannot: a flow-ref's sub-invocation roots at its own flow name, so the
 * only thing joining it to the block that called it is that it happened inside it.
 *
 * So the address is consulted first and the clock second, and when the two
 * disagree the address wins. A composite's own span covers its children, two
 * blocks share a microsecond, and a fork mints the same address on several
 * goroutines at once — the timestamps are approximate in ways the address is not.
 */

import { ancestorPaths } from "./blockPath";
import { contains, packLanes } from "./timeSpans";
import { addressKey, SELF_ADDRESSED_KINDS, type WaterfallNode } from "./types";

/**
 * Link every node to its parent, returning the roots. Fills `children`, `depth`
 * and `lane` in place; the input array is not reordered.
 */
export function nest(nodes: WaterfallNode[]): WaterfallNode[] {
  const parents = new Map<WaterfallNode, WaterfallNode>();

  // The address first, for every span that has one that resolves.
  const blocks = indexBlocks(nodes);
  for (const node of nodes) {
    const parent = trieParent(node, blocks);
    if (parent && !reaches(parent, node, parents)) parents.set(node, parent);
  }
  // Then the clock, for whatever the address could not place.
  byContainment(nodes, parents);

  const roots: WaterfallNode[] = [];
  for (const node of nodes) {
    const parent = parents.get(node);
    if (parent) parent.children.push(node);
    else roots.push(node);
  }
  order(roots, 0);
  return roots;
}

/** Every block span by the address it reported at. A fork puts several in one bucket. */
function indexBlocks(nodes: WaterfallNode[]): Map<string, WaterfallNode[]> {
  const index = new Map<string, WaterfallNode[]>();
  for (const node of nodes) {
    if (node.kind !== "block.post-invoke") continue;
    const key = addressKey(node.eventId, node.path);
    const bucket = index.get(key);
    if (bucket) bucket.push(node);
    else index.set(key, [node]);
  }
  return index;
}

/** The block this span's own address puts it inside, if one reported a span. */
function trieParent(
  node: WaterfallNode,
  blocks: Map<string, WaterfallNode[]>,
): WaterfallNode | undefined {
  if (node.path === "") return undefined;
  // A model call, or an agent compacting its own context, is stamped with its
  // block's own address rather than one below it, so the block at exactly this
  // path is its host. Every other span looks for an enclosing address, deepest
  // first.
  const ancestors = ancestorPaths(node.path).reverse();
  const candidates = SELF_ADDRESSED_KINDS.has(node.kind)
    ? [node.path, ...ancestors]
    : ancestors;

  for (const candidate of candidates) {
    const host = pickHost(blocks.get(addressKey(node.eventId, candidate)) ?? [], node);
    if (host) return host;
  }
  return undefined;
}

/**
 * Which span at one address hosted this one. Only a `fork` makes this a real
 * question: it runs the same block on several goroutines, so its invocations are
 * siblings by address and simultaneous by clock.
 */
function pickHost(
  candidates: WaterfallNode[],
  node: WaterfallNode,
): WaterfallNode | undefined {
  const others = candidates.filter((c) => c !== node);
  const enclosing = others.filter((c) => contains(c, node));
  if (enclosing.length > 0) {
    // The tightest fit: with nested forks, several may enclose it.
    return enclosing.reduce((best, c) =>
      c.end - c.start <= best.end - best.start ? c : best,
    );
  }
  if (others.length === 0) return undefined;
  // The address says this span belongs here and the clock says it does not. The
  // address wins — it is structural — so the nearest invocation takes it, and the
  // row lands under a parent it plausibly ran in rather than at the top of the
  // chart with no parent at all.
  return others.reduce((best, c) =>
    Math.abs(c.start - node.start) < Math.abs(best.start - node.start) ? c : best,
  );
}

/**
 * Place every still-unplaced span under the innermost span enclosing it, by a
 * sweep rather than a scan of every pair — a trace is capped at 20 000 records,
 * and the quadratic version of this is what would make opening the pathological
 * trace a second incident.
 *
 * Sorting widest-first means a container is always visited before what it
 * contains. Two spans measured to the very same instant are ordered by sequence,
 * descending: a composite finishes after the block inside it, so of two
 * indistinguishable spans the one published later is the outer one. That is the
 * only evidence left when the clock has none.
 */
function byContainment(
  nodes: WaterfallNode[],
  parents: Map<WaterfallNode, WaterfallNode>,
): void {
  const sorted = [...nodes].sort(
    (a, b) => a.start - b.start || b.end - a.end || b.seq - a.seq,
  );

  const open: WaterfallNode[] = [];
  for (const node of sorted) {
    while (open.length > 0 && !contains(open[open.length - 1], node)) open.pop();
    if (!parents.has(node)) {
      // The innermost enclosing span this node is not *already* above. A span
      // whose clock disagreed with its address can be measured wider than the
      // composite it ran in, and so can enclose its own ancestor; adopting it
      // there would make the tree a ring. Falling through to the next span out
      // keeps the node in the chart, which dropping it to a root would not.
      for (let i = open.length - 1; i >= 0; i--) {
        if (reaches(open[i], node, parents)) continue;
        parents.set(node, open[i]);
        break;
      }
    }
    open.push(node);
  }
}

/** Whether `target` is already an ancestor of `from`. */
function reaches(
  from: WaterfallNode,
  target: WaterfallNode,
  parents: Map<WaterfallNode, WaterfallNode>,
): boolean {
  for (let at: WaterfallNode | undefined = from; at; at = parents.get(at)) {
    if (at === target) return true;
  }
  return false;
}

/** Sort each sibling set by time, lane the concurrent ones, and stamp depth. */
function order(nodes: WaterfallNode[], depth: number): void {
  nodes.sort((a, b) => a.start - b.start || a.seq - b.seq);
  const lanes = packLanes(nodes);
  nodes.forEach((node, index) => {
    node.depth = depth;
    node.lane = lanes[index];
    order(node.children, depth + 1);
  });
}
