/**
 * Reading a block's address back out of a trace record.
 *
 * The runtime mints one path per block and ships it on every block event
 * (`runtime/core/blockpath.go` is the single definition of how one is spelled):
 *
 *     <flow>[<chain>].<block>[<branch>].<block>…
 *
 * e.g. `orders.checkHeader[else].api-call-1`. Every step but the last names a
 * composite and the branch of it to descend into; the last names the block
 * itself. That structure is what nests a waterfall: a span's parent is the block
 * whose path is its deepest ancestor. The clock cannot answer that — two blocks
 * can share a millisecond, and a composite's own span covers its children — so
 * the address is the structural answer and the timestamps are only corroboration.
 *
 * ## What this parser refuses to guess
 *
 * A block is addressed by its name, else its type, else its ref, with no check
 * that the result is spellable. A block *named* `a.b` therefore mints a path this
 * grammar cannot distinguish from a block `b` inside a composite `a` — which is
 * why the editor refuses to mint an address for such a block at all (see
 * `isCleanSegment` in packages/editor/src/app/run/address.ts). Here the record has
 * already been written, so refusing is not on offer.
 *
 * So the rule is: parse mechanically, never throw, and read a branch only from a
 * segment that is exactly `label[branch]`. Anything else stays in the label, where
 * it renders as a slightly odd block name rather than as a wrong tree. A waterfall
 * that draws one strange row is recoverable; one that reparents a block under a
 * composite it never ran in is a lie about what happened.
 */

/** One step of a path: a block, and the branch of it the next step descends into. */
export interface PathSegment {
  /** How the block is addressed: its name, else its type, else the ref it points at. */
  label: string;
  /** The branch descended into, or null on the last step and on plain blocks. */
  branch: string | null;
  /**
   * This block's own path — the prefix of the original string up to and including
   * its label, branch excluded. It is sliced from the input rather than rebuilt by
   * joining, so it is byte-for-byte the path the record for that block carries.
   */
  path: string;
}

/**
 * Split a path into its steps, outermost first. An empty path (which every flow
 * and source record carries — those name no block) yields no steps.
 */
export function parseBlockPath(path: string): PathSegment[] {
  if (path === "") return [];

  const segments: PathSegment[] = [];
  // Where the current segment starts, the depth-0 "[" in it, and one past the "]"
  // that closed back to depth 0. The last two stay -1 until they are seen.
  let start = 0;
  let open = -1;
  let close = -1;
  let depth = 0;

  const flush = (end: number) => {
    // A branch is read only from a segment spelled exactly `label[branch]`: the
    // bracket opened at depth 0 and closed at the very end. An unterminated
    // bracket, a stray "]", or trailing text after one all fall through to "the
    // whole thing is the label", which is the graceful degradation this file
    // exists to guarantee.
    const clean = open >= 0 && close === end;
    const stop = clean ? open : end;
    segments.push({
      label: path.slice(start, stop),
      branch: clean ? path.slice(open + 1, end - 1) : null,
      path: path.slice(0, stop),
    });
  };

  for (let i = 0; i < path.length; i++) {
    switch (path[i]) {
      case "[":
        if (depth === 0 && open < 0) open = i;
        depth++;
        break;
      case "]":
        // Guarded: a "]" with nothing open is just a character in a block's name.
        if (depth > 0 && --depth === 0) close = i + 1;
        break;
      case ".":
        // Only a "." outside every bracket separates steps, so a branch named
        // `a.b` stays one branch.
        if (depth === 0) {
          flush(i);
          start = i + 1;
          open = close = -1;
        }
        break;
    }
  }
  flush(path.length);

  return segments;
}

/**
 * The paths of every block enclosing `path`, outermost first — what a nesting pass
 * walks from the deepest end to find the parent that actually reported a span.
 *
 * `orders.checkHeader[else].api-call-1` encloses `orders` (the flow, which reports
 * no block span of its own) and `orders.checkHeader`.
 */
export function ancestorPaths(path: string): string[] {
  const segments = parseBlockPath(path);
  return segments.slice(0, -1).map((s) => s.path);
}

/**
 * Whether `inner` addresses a block inside the block `outer` addresses. Mirrors
 * `isInside` in runtime/core/runtime/mock.go and in the editor's address.ts.
 *
 * Defined in terms of {@link ancestorPaths} on purpose: the containment test and
 * the tree the waterfall builds are then the same function, so they cannot come to
 * different conclusions about the same two blocks.
 */
export function isInside(outer: string, inner: string): boolean {
  return outer !== "" && ancestorPaths(inner).includes(outer);
}

/**
 * How a block is labelled in the waterfall: the last step of its path. Empty for a
 * record that names no block.
 */
export function blockLabel(path: string): string {
  const segments = parseBlockPath(path);
  return segments.length === 0 ? "" : segments[segments.length - 1].label;
}
