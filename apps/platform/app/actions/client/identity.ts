/**
 * Health: the call that answers "is the orchestrator there". Kept apart from the
 * resource CRUD because it is what the app needs before any of it.
 *
 * Users and their API keys are not here. They belong to iam, which owns the
 * identity this platform authorizes on — see app/actions/client/iam.ts and
 * app/actions/client/apikeys.ts.
 */

import { baseUrl } from "./http";
import { requestOk } from "@octo/http";

// --- Health ---------------------------------------------------------------

/**
 * Whether the orchestrator is configured and answers its health check. Uses a
 * body-less probe (the `/healthz` response is plain text "ok", not JSON), so it
 * doesn't go through the JSON client.
 */
export function checkHealth(): Promise<boolean> {
  const base = baseUrl();
  if (!base) return Promise.resolve(false);
  return requestOk("GET", `${base}/healthz`);
}
