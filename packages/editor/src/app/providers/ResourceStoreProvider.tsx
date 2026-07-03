"use client";

import { createContext, useContext, type ReactNode } from "react";

/**
 * The editor's resource-store capability: the CRUD backing for the Resources tab,
 * decoupled from where resource *content* lives. A resource is an external file a
 * config imports at deploy/load time — an env file combined into the environment
 * (`kind: "env"`) or a text template rendered by the `template-resource` block
 * (`kind: "template"`). Names are path-like and may contain `/`; a resource is
 * addressed by its uuid, never by a path segment.
 *
 * The platform backs this with the orchestrator (via the resource server actions);
 * a standalone app could back it with local disk. Editor components read it through
 * {@link useResourceStore}; when it is null the Resources tab is hidden — the store
 * is the source of truth for content, distinct from the document's declared
 * `resources:` references (which serialize into the YAML).
 */

/** A stored resource: name-keyed content plus its runtime kind and uuid. */
export interface StoredResource {
  /** Stable uuid the backend addresses the resource by. */
  id: string;
  /** Path-like name (may contain `/`), e.g. `.env.staging`, `templates/x.tmpl`. */
  name: string;
  /** Runtime kind: an env file or a rendered template. */
  kind: "env" | "template";
  /** The raw file content. */
  content: string;
}

/** Body for creating a resource. */
export interface CreateResourceInput {
  name: string;
  kind: "env" | "template";
  content: string;
}

/** A patch applied to an existing resource's content (and/or kind). */
export interface UpdateResourcePatch {
  kind?: "env" | "template";
  content?: string;
}

/** What the Resources tab needs to browse and mutate resource content. */
export interface ResourceStore {
  /** List the integration's resources, content included. */
  list(): Promise<StoredResource[]>;
  /** Create a resource; returns the stored record (with its minted uuid). */
  create(input: CreateResourceInput): Promise<StoredResource>;
  /** Update a resource's content/kind by id; returns the stored record. */
  update(id: string, patch: UpdateResourcePatch): Promise<StoredResource>;
  /**
   * Rename/move a resource to `name`, keeping its content. Returned as its own
   * operation (not an `update`) because a rename can change the record's identity
   * — a disk-backed store keys by name, so the id itself changes. Returns the
   * moved record so the caller can swap it by its previous id.
   */
  move(id: string, name: string): Promise<StoredResource>;
  /** Delete a resource by id. */
  remove(id: string): Promise<void>;
}

const ResourceStoreContext = createContext<ResourceStore | null>(null);

export function ResourceStoreProvider({
  value,
  children,
}: {
  value: ResourceStore | null;
  children: ReactNode;
}) {
  return (
    <ResourceStoreContext.Provider value={value}>
      {children}
    </ResourceStoreContext.Provider>
  );
}

/** The current resource store, or null when none is provided. */
export function useResourceStore(): ResourceStore | null {
  return useContext(ResourceStoreContext);
}
