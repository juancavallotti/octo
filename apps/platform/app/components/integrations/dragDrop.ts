import {
  isDescendant,
  type DragData,
  type DropData,
  type FlatFolder,
} from "./model";

/**
 * What a drop should do — decided, not done.
 *
 * The tree accepts several kinds of drag onto several kinds of target, and many
 * pairs mean nothing: an unfiled integration onto the top level, anything onto
 * itself. Separating the decision from the mutation keeps those rules
 * checkable — not least that a folder dropped into its own subtree would detach
 * that subtree from the tree entirely.
 */
export type DragOutcome =
  | { kind: "none" }
  /** Move an integration before another in its folder. */
  | { kind: "reorder-integration"; id: string; beforeId: string }
  | { kind: "file-integration"; folderId: string; integrationId: string }
  | { kind: "unfile-integration"; folderId: string; integrationId: string }
  /** Move a folder among its siblings. */
  | { kind: "reorder-folder"; parentId: string | null; id: string; beforeId: string }
  /** Move a folder under a new parent, or to the root when parentId is null. */
  | { kind: "reparent-folder"; id: string; name: string; parentId: string | null };

const NONE: DragOutcome = { kind: "none" };

/**
 * Resolve a drop into the one thing it should do.
 *
 * `over` is either a drop zone (DropData) or another row of the tree (DragData)
 * — the two arrive through the same channel, so the shape has to be
 * discriminated rather than assumed.
 */
export function resolveDrag(
  active: DragData | undefined,
  over: DropData | DragData | undefined,
  ctx: {
    /** Folders flattened depth-first, used for the descendant check. */
    flat: FlatFolder[];
    /** Which folder each integration currently sits in, if any. */
    membership: ReadonlyMap<string, string | null>;
  },
): DragOutcome {
  if (!active || !over) return NONE;

  return active.kind === "integration"
    ? resolveIntegrationDrop(active, over, ctx.membership)
    : resolveFolderDrop(active, over, ctx.flat);
}

function resolveIntegrationDrop(
  active: Extract<DragData, { kind: "integration" }>,
  over: DropData | DragData,
  membership: ReadonlyMap<string, string | null>,
): DragOutcome {
  const current = membership.get(active.id) ?? null;

  // Dropped on another integration: put it where that one is. Beside a peer in
  // the same folder that is a reorder; in another folder it is a move there; at
  // the top level it is an unfile.
  if (over.kind === "integration") {
    if (over.id === active.id) return NONE;
    const target = membership.get(over.id) ?? null;
    if (target === current) {
      // The top level keeps no order of its own, so there is nothing to reorder.
      return current === null
        ? NONE
        : { kind: "reorder-integration", id: active.id, beforeId: over.id };
    }
    if (target !== null) {
      return { kind: "file-integration", folderId: target, integrationId: active.id };
    }
    return current
      ? { kind: "unfile-integration", folderId: current, integrationId: active.id }
      : NONE;
  }

  if (over.kind === "folder") {
    // Already there: a drop that changes nothing is not a write.
    return over.id === current
      ? NONE
      : { kind: "file-integration", folderId: over.id, integrationId: active.id };
  }
  // The top level: out of its folder, if it is in one.
  return current
    ? { kind: "unfile-integration", folderId: current, integrationId: active.id }
    : NONE;
}

function resolveFolderDrop(
  active: Extract<DragData, { kind: "folder" }>,
  over: DropData | DragData,
  flat: FlatFolder[],
): DragOutcome {
  const folder = flat.find((x) => x.id === active.id);
  if (!folder) return NONE;
  const parentId = folder.parentId ?? null;

  if (over.kind === "folder") {
    if (over.id === active.id) return NONE;
    const target = flat.find((x) => x.id === over.id);
    if (!target) return NONE;

    // Onto a sibling: reorder within the group. Onto a folder in another group:
    // reparent under it — unless that folder is inside this one, which would cut
    // the subtree loose from the tree.
    if ((target.parentId ?? null) === parentId) {
      return { kind: "reorder-folder", parentId, id: active.id, beforeId: over.id };
    }
    return isDescendant(flat, over.id, active.id)
      ? NONE
      : { kind: "reparent-folder", id: active.id, name: folder.name, parentId: over.id };
  }

  // Onto the top level: lift it there, unless it is already there.
  if (over.kind === "root" && parentId !== null) {
    return { kind: "reparent-folder", id: active.id, name: folder.name, parentId: null };
  }
  return NONE;
}
