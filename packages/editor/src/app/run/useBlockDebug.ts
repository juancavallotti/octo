"use client";

import { useCallback, useMemo } from "react";
import { EditorActionType, useEditorState } from "../state/editorState";
import { useEditorMeta } from "../providers/EditorMetaProvider";
import { useFlowRun } from "./FlowRunContext";
import { namesNeededFor, naturalAddress, type BlockRename } from "./address";
import type { BlockNode, EditorDocument } from "../model/document";
import type { BlockMock } from "../meta/types";
import type { SpyRecord } from "./transport";

/** Whether `blocks`, or anything nested under them, holds the block with `blockId`. */
function holds(blocks: BlockNode[], blockId: string): boolean {
  return blocks.some(
    (b) =>
      b.id === blockId ||
      Object.values(b.slots ?? {}).some((subs) =>
        subs.some((sub) => holds(sub.process, blockId)),
      ),
  );
}

/** The id of the top-level flow the block sits in, at any depth. */
function rootFlowIdOf(doc: EditorDocument, blockId: string): string | null {
  for (const flow of doc.flows) {
    if (holds(flow.process, blockId) || holds(flow.error?.process ?? [], blockId)) {
      return flow.id;
    }
  }
  return null;
}

/**
 * Everything the mock and spy affordances on a node need, in one place: the block's
 * address, whether it is mocked or spied, and how to change that.
 *
 * ## Naming, and why it happens here
 *
 * A mock and a spy are keyed by the block's address, which has to survive being written
 * to a file and read back tomorrow. A block whose label a sibling shares has no such
 * address (see address.ts), so *placing* a mock or spy on one first gives it a real
 * name — a visible edit to the document, made by {@link ensureAddress} on the way.
 *
 * Doing it at placement rather than at run time is what makes the key trustworthy: by the
 * time anything is saved under an address, that address is already in the document
 * everyone can see, and the user can rename it to something better if they don't like
 * what we picked.
 */
export interface BlockDebug {
  /**
   * The block's address as the document stands, or null when it has none yet — either
   * because naming would fix it (and hasn't) or because nothing can.
   */
  address: string | null;
  /**
   * Whether this block can carry a mock or a spy at all. False when no name could make it
   * addressable — an unaddressable branch, a flow whose name breaks the grammar — in which
   * case the affordances hide themselves rather than offer something that cannot work,
   * exactly as run-to-here already does.
   */
  supported: boolean;
  /** The mock on this block, enabled or not. */
  mock: BlockMock | undefined;
  /** Whether a spy is watching this block. */
  spied: boolean;
  /** What the spy has collected, across every run since it was last cleared. */
  records: SpyRecord[];

  /**
   * Give the block a durable address, naming it (and any ambiguous composite above it) if
   * that is what it takes, and return it. Null when nothing can.
   *
   * Call this before saving anything against the address — the document edit and the save
   * are one gesture, and an address obtained any other way may not exist yet.
   */
  ensureAddress(): string | null;
  /** Save the mock, placing it on the block's address. */
  setMock(mock: Omit<BlockMock, "address">): void;
  removeMock(): void;
  toggleSpy(): void;
  clearRecords(): void;
}

export function useBlockDebug(blockId: string): BlockDebug | null {
  const { state, dispatch } = useEditorState();
  const meta = useEditorMeta();
  const flowRun = useFlowRun();
  const doc = state.document;

  // The TOP-LEVEL flow the block ultimately sits in — what its meta entry is filed under.
  // A block nested in a composite belongs to a sub-flow, which is not separately runnable
  // and has no entry of its own.
  const flowId = useMemo(() => rootFlowIdOf(doc, blockId), [doc, blockId]);

  const address = useMemo(() => naturalAddress(doc, blockId), [doc, blockId]);
  const supported = useMemo(() => namesNeededFor(doc, blockId) !== null, [doc, blockId]);

  const ensureAddress = useCallback((): string | null => {
    const renames = namesNeededFor(doc, blockId);
    if (renames === null) return null;
    for (const rename of renames) {
      dispatch({
        type: EditorActionType.RENAME_BLOCK,
        data: { blockId: rename.blockId, name: rename.name },
      });
    }
    // The address is derived from the names we just asked for, not read back from the
    // document: the reducer has not run yet, so `doc` here is still the old one.
    return addressAfter(doc, blockId, renames);
  }, [doc, blockId, dispatch]);

  const mock = flowId && address ? meta?.mockAt(flowId, address) : undefined;
  const spied = !!flowId && !!address && (meta?.spies(flowId) ?? []).includes(address);
  const records = address ? (flowRun?.spyRecords(address) ?? []) : [];

  if (!meta || !flowId) return null;

  return {
    address,
    supported,
    mock,
    spied,
    records,
    ensureAddress,
    setMock(next) {
      const at = ensureAddress();
      if (at) meta.setMock(flowId, { ...next, address: at });
    },
    removeMock() {
      if (address) meta.removeMock(flowId, address);
    },
    toggleSpy() {
      const at = ensureAddress();
      if (at) meta.setSpy(flowId, at, !spied);
    },
    clearRecords() {
      if (address) flowRun?.clearSpies(address);
    },
  };
}

/**
 * The address the block WILL have once `renames` are applied, computed against a copy of
 * the document with those names already in it.
 *
 * This exists because a dispatch is not synchronous: the reducer has not run when
 * `ensureAddress` returns, so reading the address off the live document would give the
 * old one — or null, for the very block we just named.
 */
function addressAfter(
  doc: EditorDocument,
  blockId: string,
  renames: BlockRename[],
): string | null {
  if (renames.length === 0) return naturalAddress(doc, blockId);

  const named = new Map(renames.map((r) => [r.blockId, r.name]));
  const clone = structuredClone(doc);
  const apply = (blocks: BlockNode[]) => {
    for (const block of blocks) {
      const name = named.get(block.id);
      if (name) block.name = name;
      for (const subs of Object.values(block.slots ?? {})) {
        for (const sub of subs) apply(sub.process);
      }
    }
  };
  for (const flow of clone.flows) {
    apply(flow.process);
    apply(flow.error?.process ?? []);
  }
  return naturalAddress(clone, blockId);
}
