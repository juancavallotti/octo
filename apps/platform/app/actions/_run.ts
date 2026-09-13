/**
 * The non-action helpers behind `run.ts`.
 *
 * They live here because `run.ts` carries "use server", which restricts a module
 * to async exports: a helper kept there is legal only for as long as it stays
 * unexported. The `_` prefix matches the folder's other non-action modules.
 */

import { binaries, type RunKey, type RunState } from "@octo/run-host";
import type { RunStatusSnapshot } from "@octo/editor";
import { ensureRunNamespace } from "@/app/run/namespace";
import { orchestratorResourceProvider } from "@/app/lib/runResources";

/**
 * The editor snapshot: what this host can spawn (its binaries, which back the one-shot
 * debug runs) and what the app runner is currently doing. Separate sources — a binary
 * is installed on the host, a run belongs to a backend.
 */
export function snapshotOf(state: RunState): RunStatusSnapshot {
  return { ...binaries(), ...state };
}

/** The resource provider for a run, or undefined for an unsaved draft (no id). */
export function resourcesFor(integrationId?: unknown) {
  if (typeof integrationId !== "string" || integrationId.trim() === "") return undefined;
  return orchestratorResourceProvider(integrationId);
}

/**
 * The key the app runner addresses: the owning user and integration, which is what a dev
 * run *is*, plus the run namespace, which it ignores.
 *
 * The namespace is resolved anyway because `ensureRunNamespace` is what mints the run
 * cookie, and the one-shots key their staging directories on it.
 */
export async function runKey(
  tabId: string,
  userId: string,
  integrationId?: string,
): Promise<RunKey> {
  return { namespace: await ensureRunNamespace(tabId), userId, integrationId };
}
