"use client";

import { createContext, useContext, type ReactNode } from "react";

/**
 * The editor's load/save capability, decoupled from where documents live. The
 * platform backs it with the orchestrator (via the BFF); a standalone app backs
 * it with the local disk. Editor components read it through `useFileSystem()`;
 * when it is null the load/save controls render nothing. Folder organization is
 * an optional sub-capability — absent it, the folder picker hides.
 */

/** A stored document: a named flow definition (runtime YAML) plus bookkeeping. */
export interface StoredDocument {
  /** Platform: the integration id. Standalone: the file path. */
  id: string;
  name: string;
  /** The flow definition, as the runtime YAML the editor serializes. */
  definition: string;
  /** Owning folder, when the backend organizes documents into folders. */
  folderId?: string | null;
  /** RFC3339 timestamp of the last update, when known. */
  lastUpdated?: string;
}

/** Body for creating/updating a document. */
export interface SaveInput {
  name: string;
  definition: string;
  /** Folder to file a newly created document under, when supported. */
  folderId?: string | null;
}

/** A folder in the organization tree. */
export interface FolderNode {
  id: string;
  parentId: string | null;
  name: string;
  children?: FolderNode[];
}

/** Optional folder organization, surfaced by backends that support it. */
export interface FolderCapability {
  /** The folder tree. */
  list(): Promise<FolderNode[]>;
  /** File a document under a folder (single-membership: replaces any prior). */
  assign(folderId: string, documentId: string): Promise<void>;
  /** Remove a document from a folder. */
  unassign(folderId: string, documentId: string): Promise<void>;
}

/** What the editor needs to load and persist documents. */
export interface FileSystemCapability {
  /**
   * Why this backend will not accept a save, when it will not — a sentence for
   * the control that offers one. Absent means writable.
   *
   * Documents still load and read: a backend that declines writes is not the
   * same as no capability at all, which hides the load and save controls
   * together.
   */
  readOnly?: string;
  load(id: string): Promise<StoredDocument>;
  /** Create when `id` is null, update otherwise; returns the stored record. */
  save(id: string | null, input: SaveInput): Promise<StoredDocument>;
  /** List stored documents, when the backend supports browsing. */
  list?(): Promise<StoredDocument[]>;
  /** Folder organization, when supported. */
  folders?: FolderCapability;
}

const FileSystemContext = createContext<FileSystemCapability | null>(null);

export function FileSystemProvider({
  value,
  children,
}: {
  value: FileSystemCapability | null;
  children: ReactNode;
}) {
  return (
    <FileSystemContext.Provider value={value}>
      {children}
    </FileSystemContext.Provider>
  );
}

/** The current filesystem capability, or null when none is provided. */
export function useFileSystem(): FileSystemCapability | null {
  return useContext(FileSystemContext);
}
