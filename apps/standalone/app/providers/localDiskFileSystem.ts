/**
 * The standalone FileSystemCapability: load/save flows via server actions that
 * read and write `*.yaml` files in a local directory. Flat storage, so no folder
 * capability — the editor's folder UI stays hidden.
 */

import type { FileSystemCapability, StoredDocument } from "@octo/editor";
import { createFlow, listFlows, loadFlow, saveFlow } from "../actions/fs";
import { unwrap } from "../actions/result";

export const localDiskFileSystem: FileSystemCapability = {
  async load(id) {
    return unwrap(await loadFlow(id));
  },

  async save(id, input) {
    // With an id the store may rename the file when the slug changes, so the
    // result can carry a new id (the editor adopts it); without one we create.
    return id
      ? unwrap(await saveFlow(id, input.name, input.definition))
      : unwrap(await createFlow(input.name, input.definition));
  },

  async list() {
    // The list carries id + name only (no definition); the editor uses just those.
    return unwrap(await listFlows()) as unknown as StoredDocument[];
  },
};
