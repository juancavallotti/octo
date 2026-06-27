"use server";

/**
 * Server actions for the standalone filesystem — the replacement for the
 * `/api/fs*` route handlers. They drive the in-process disk store directly (no
 * HTTP, no auth: standalone is local-only). Each returns an ActionResult; the
 * editor's FileSystemCapability (localDiskFileSystem) unwraps it.
 */

import type { ActionResult } from "@octo/http";
import * as store from "../api/fs/store";
import type { FlowDoc } from "../api/fs/store";

/** List the stored flows ({ id, name }). */
export async function listFlows(): Promise<
  ActionResult<{ id: string; name: string }[]>
> {
  try {
    return { ok: true, data: await store.listFlows() };
  } catch (err) {
    return { ok: false, error: (err as Error).message };
  }
}

/** Read one flow ({ id, name, definition }). */
export async function loadFlow(id: string): Promise<ActionResult<FlowDoc>> {
  if (!id) return { ok: false, error: "missing `path`" };
  try {
    return { ok: true, data: await store.readFlow(id) };
  } catch (err) {
    return { ok: false, error: (err as Error).message };
  }
}

/** Create a new flow file. */
export async function createFlow(
  name: string,
  definition: string,
): Promise<ActionResult<FlowDoc>> {
  if (typeof definition !== "string") {
    return { ok: false, error: "missing `definition`" };
  }
  try {
    return { ok: true, data: await store.createFlow(name, definition) };
  } catch (err) {
    return { ok: false, error: (err as Error).message };
  }
}

/**
 * Overwrite an existing flow. When `name` is given and its slug differs from the
 * current filename the flow is renamed on disk (the result carries the new id).
 */
export async function saveFlow(
  id: string,
  name: string | undefined,
  definition: string,
): Promise<ActionResult<FlowDoc>> {
  if (!id) return { ok: false, error: "missing `path`" };
  if (typeof definition !== "string") {
    return { ok: false, error: "missing `definition`" };
  }
  try {
    const stored =
      typeof name === "string"
        ? await store.updateFlow(id, name, definition)
        : await store.writeFlow(id, definition);
    return { ok: true, data: stored };
  } catch (err) {
    return { ok: false, error: (err as Error).message };
  }
}
