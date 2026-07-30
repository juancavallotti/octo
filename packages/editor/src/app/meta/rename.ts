import type { EditorDocument } from "../model/document";
import type { FileMeta, FlowMeta } from "./types";

/**
 * Keeping the meta file in step with a flow that gets renamed.
 *
 * The file keys a flow's saved inputs by its NAME, because that is the only identity
 * that survives being written to disk — the editor's client ids are minted fresh by
 * `crypto.randomUUID()` on every parse. Rename a flow and the key would be orphaned.
 *
 * Within a session, though, a client id *is* stable: the reducer mutates the flow in
 * place, so the same id carries the old name and then the new one. That is enough to
 * detect a rename by diffing two id→name maps taken at different moments.
 *
 * The nice property of doing it this way is what happens on a *reload*, where every id
 * is new: the two maps then share no ids at all, so no id qualifies as "renamed" and
 * the sync is a structural no-op. The mechanism cannot misfire on the very case that
 * makes ids untrustworthy.
 *
 * A rename moves more than the key. Mocks and spies are addressed by the runtime's block
 * path, whose FIRST SEGMENT is the flow's name — `orders.charge`, `orders[error].notify`
 * — so renaming the flow invalidates every address inside its entry too. Moving the key
 * and leaving the addresses would be worse than doing nothing: the mocks would look
 * present in the file and on the canvas, and silently never fire.
 */

/** The name of each top-level flow, by client id. Sub-flows have no saved inputs. */
export function flowIdNames(doc: EditorDocument): Map<string, string> {
  const names = new Map<string, string>();
  for (const flow of doc.flows) names.set(flow.id, flow.name);
  return names;
}

/**
 * Re-root one block address at a renamed flow. An address opens with the flow's name,
 * optionally carrying its chain — `orders.charge`, `orders[error].notify` — so only that
 * first segment moves; everything below it names blocks, which the rename did not touch.
 *
 * An address not rooted at `before` is returned as it was.
 *
 * Exported because the editor is not the only thing that renames a flow: an MCP agent's
 * `update_flow` does too, and a second implementation of this rule would be a second
 * chance to get it wrong — silently, since a stale address still *looks* like a mock.
 */
export function readdress(address: string, before: string, after: string): string {
  if (address === before) return after;
  for (const sep of [".", "["]) {
    if (address.startsWith(before + sep)) {
      return after + address.slice(before.length);
    }
  }
  return address;
}

/**
 * Re-root every mock and spy in a flow's entry at its new name. Returns the entry
 * unchanged — the same object — when no address was rooted at `before`, so a caller can
 * tell "nothing moved" by identity rather than by comparing.
 */
function readdressEntry(entry: FlowMeta, before: string, after: string): FlowMeta {
  const mocks = entry.mocks?.map((m) => {
    const address = readdress(m.address, before, after);
    return address === m.address ? m : { ...m, address };
  });
  const spies = entry.spies?.map((s) => readdress(s, before, after));

  const mocksMoved = !!mocks && mocks.some((m, i) => m !== entry.mocks![i]);
  const spiesMoved = !!spies && spies.some((s, i) => s !== entry.spies![i]);
  if (!mocksMoved && !spiesMoved) return entry;

  return {
    ...entry,
    ...(mocks ? { mocks } : {}),
    ...(spies ? { spies } : {}),
  };
}

/**
 * Move one flow's entry to its new name and re-root every address that named it.
 *
 * The whole rename, by name alone — which is how anything other than the editor's
 * reducer knows about one. An agent that renames a flow through MCP has two names and no
 * client ids, and this is the same code path {@link syncFlowNames} takes, so the file
 * comes out the same whichever end asked for it.
 *
 * A rename onto a name already in use is refused: two flows cannot share one in a valid
 * document, and merging their entries would be worse than leaving the stale key for the
 * (invalid) document to resolve.
 *
 * The addresses are re-rooted across EVERY entry, not just the renamed flow's — an
 * address names a block by a path starting at a flow, and it means that flow wherever the
 * address happens to be filed.
 */
export function renameFlow(
  meta: FileMeta,
  before: string,
  after: string,
): { meta: FileMeta; changed: boolean } {
  if (before === after || after in meta.flows) return { meta, changed: false };

  let changed = false;
  const flows = { ...meta.flows };
  if (before in flows) {
    flows[after] = flows[before];
    delete flows[before];
    changed = true;
  }

  // Even with nothing saved under the old name, another entry may hold an address into
  // the flow that just got renamed — so this runs whether or not the key moved.
  const readdressed: Record<string, FlowMeta> = {};
  for (const [name, entry] of Object.entries(flows)) {
    const next = readdressEntry(entry, before, after);
    if (next !== entry) changed = true;
    readdressed[name] = next;
  }

  return changed ? { meta: { ...meta, flows: readdressed }, changed } : { meta, changed: false };
}

/**
 * Move each renamed flow's entry to its new key. Only ids present in *both* maps count
 * as a rename: an id only in `prev` was deleted (its entry is left alone rather than
 * dropped — a flow removed by accident should not take its test inputs with it), and
 * an id only in `next` is new.
 *
 * Each one is then a {@link renameFlow}, which is where the rules about collisions and
 * addresses live.
 */
export function syncFlowNames(
  meta: FileMeta,
  prev: Map<string, string>,
  next: Map<string, string>,
): { meta: FileMeta; changed: boolean } {
  let current = meta;
  let changed = false;

  for (const [id, before] of prev) {
    const after = next.get(id);
    if (after === undefined || after === before) continue; // gone, or not renamed
    const step = renameFlow(current, before, after);
    if (step.changed) {
      current = step.meta;
      changed = true;
    }
  }

  return { meta: current, changed };
}
