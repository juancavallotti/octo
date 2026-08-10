/**
 * The non-action helpers behind `run.ts`.
 *
 * They live here rather than in `run.ts` because that file carries "use server",
 * which restricts a module to async exports: a helper kept there is legal only
 * for as long as it stays unexported, and turns into a build error the moment
 * someone reaches for it from a second file. Splitting them out removes the trap
 * and, incidentally, the last line `run.ts` was over its cap by.
 *
 * The `_` prefix matches the folder's other non-action modules — `_auth`,
 * `_client`, `_logs`, `_nats`, `_traces`.
 */

import { binaries, type RunKey, type RunState } from "@octo/run-host";
import type { RunStatusSnapshot } from "@octo/editor";
import { ensureRunNamespace } from "@/app/run/namespace";
import { orchestratorResourceProvider } from "@/app/lib/runResources";

/**
 * The editor snapshot, composed from the two things it asks about at once: what this
 * host can spawn (its binaries, which back the one-shot debug runs) and what the app
 * runner is currently doing. They are separate because they answer to different owners
 * — a binary is installed on the host, a run belongs to a backend.
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
 * The namespace is resolved anyway rather than skipped, and not out of politeness to the
 * type: `ensureRunNamespace` is what mints the run cookie, and the one-shots below key
 * their staging directories on it. Dropping it here would leave the first `invoke` of a
 * session to mint it instead — which works, and hides why it has to happen at all.
 */
export async function runKey(
  tabId: string,
  userId: string,
  integrationId?: string,
): Promise<RunKey> {
  return { namespace: await ensureRunNamespace(tabId), userId, integrationId };
}
