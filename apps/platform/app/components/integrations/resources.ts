import type { Resource, SnapshotResource } from "@/app/model/orchestrator";

/**
 * The pure half of the resources panel: what a resource looks like in the list,
 * and the two ways one arrives. React-free, so the kind-guessing rule can be
 * exercised directly rather than only through a form.
 */

/** One row of the resources list, from either source. */
export interface DisplayResource {
  key: string;
  kind: string;
  name: string;
  /** Present only for live resources; frozen ones can't be deleted. */
  id?: string;
  /** The content, for a live resource — the list already carries it, so
   * downloading one costs nothing. A frozen resource is metadata only and its
   * content is fetched on demand. */
  content?: string;
}

export const KINDS = ["env", "template"] as const;
export type Kind = (typeof KINDS)[number];

/**
 * Guess a resource's kind from its name the way the standalone loader does: a
 * `.env`-convention file is env, everything else is a template.
 *
 * The name may be a path — resource names carry their relative position for
 * bundle uploads — so only the last segment decides.
 */
export function guessKind(name: string): Kind {
  const base = name.split("/").pop() ?? name;
  return base.startsWith(".env") ? "env" : "template";
}

/** A live working-copy resource: it has an id, so it can be deleted. */
export function fromLive(r: Resource): DisplayResource {
  return {
    key: r.id,
    id: r.id,
    kind: r.kind,
    name: r.name,
    content: r.content,
  };
}

/**
 * A resource frozen under a version tag. It has no id: it belongs to the snapshot
 * rather than having an identity of its own, and the absent id is what marks it
 * undeletable.
 */
export function fromFrozen(r: SnapshotResource): DisplayResource {
  return { key: `${r.kind}:${r.name}`, kind: r.kind, name: r.name };
}
