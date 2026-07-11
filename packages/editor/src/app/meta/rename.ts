import type { EditorDocument } from "../model/document";
import type { FileMeta } from "./types";

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
 */

/** The name of each top-level flow, by client id. Sub-flows have no saved inputs. */
export function flowIdNames(doc: EditorDocument): Map<string, string> {
  const names = new Map<string, string>();
  for (const flow of doc.flows) names.set(flow.id, flow.name);
  return names;
}

/**
 * Move each renamed flow's entry to its new key. Only ids present in *both* maps count
 * as a rename: an id only in `prev` was deleted (its entry is left alone rather than
 * dropped — a flow removed by accident should not take its test inputs with it), and
 * an id only in `next` is new.
 *
 * A rename that would land on a key already in use is skipped: two flows cannot share
 * a name in a valid document, and silently merging their inputs would be worse than
 * leaving the stale key for the (invalid) document to resolve.
 */
export function syncFlowNames(
  meta: FileMeta,
  prev: Map<string, string>,
  next: Map<string, string>,
): { meta: FileMeta; changed: boolean } {
  const flows = { ...meta.flows };
  let changed = false;

  for (const [id, before] of prev) {
    const after = next.get(id);
    if (after === undefined || after === before) continue; // gone, or not renamed
    if (!(before in flows)) continue; // nothing saved under the old name
    if (after in flows) continue; // name collision: leave it to the user to resolve

    flows[after] = flows[before];
    delete flows[before];
    changed = true;
  }

  return changed ? { meta: { flows }, changed } : { meta, changed: false };
}
