/**
 * Where iam is — the platform's identity service, and the only component that
 * talks to the identity provider. It converts what the provider says into a token
 * signed by this platform, carrying the caller's octo user id and their roles;
 * sign-in and every re-mint of a session's token go through it.
 *
 * Unset means sign-in cannot complete, which is not the same as "the feature is
 * off": a session with no platform token cannot call anything, so the sign-in is
 * refused rather than half-completed. See app/auth/octoToken.ts.
 */

import type { ActionResult } from "@octo/http";

/** The env var that carries the address — the chart sets it from the iam Service. */
const envVar = "IAM_URL";

/** iam's base URL with any trailing slash trimmed, or "" when unset. */
export function iamBaseUrl(): string {
  return (process.env[envVar] ?? "").replace(/\/+$/, "");
}

/** The error result a client returns for `feature` when the address is unset. */
export function iamUnconfigured<T>(feature: string): ActionResult<T> {
  return { ok: false, error: `${feature} not configured (${envVar} unset)` };
}
