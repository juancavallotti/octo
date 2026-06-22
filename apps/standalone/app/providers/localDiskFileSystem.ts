/**
 * The standalone FileSystemCapability: load/save flows against this app's local
 * `/api/fs` routes, which read and write `*.yaml` files in a local directory.
 * Flat storage, so no folder capability — the editor's folder UI stays hidden.
 */

import type {
  FileSystemCapability,
  StoredDocument,
} from "@octo/editor";

async function json<T>(res: Response): Promise<T> {
  const body = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error((body as { error?: string }).error ?? res.statusText);
  }
  return body as T;
}

export const localDiskFileSystem: FileSystemCapability = {
  async load(id) {
    const res = await fetch(`/api/fs/file?path=${encodeURIComponent(id)}`);
    return json<StoredDocument>(res);
  },

  async save(id, input) {
    if (id) {
      const res = await fetch(`/api/fs/file?path=${encodeURIComponent(id)}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ definition: input.definition }),
      });
      return json<StoredDocument>(res);
    }
    const res = await fetch("/api/fs", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: input.name, definition: input.definition }),
    });
    return json<StoredDocument>(res);
  },

  async list() {
    const res = await fetch("/api/fs");
    return json<StoredDocument[]>(res);
  },
};
