/**
 * Reading the platform token back out of the session cookie.
 *
 * Its own module rather than part of octoToken.ts for two reasons. It is the only
 * thing here that touches Auth.js's cookie internals, so the policy beside it
 * stays testable without them — `next-auth/jwt` pulls in `next/server`, which a
 * plain unit test has no business loading. And the two are different jobs: one
 * decides when a token is renewed, this one hands the current one to something
 * that has to present it.
 */

import { headers } from "next/headers";
import { getToken } from "next-auth/jwt";
import { authEnabled } from "@/auth";
import type { OctoTokenFields } from "./octoToken";

/**
 * The caller's platform token, for a server-side call that has to present it.
 *
 * It is read back out of the session cookie rather than off the session object,
 * because it was deliberately never put on the session object — see the note at
 * the top of this file. `getToken` is the supported way to do that: it works out
 * the cookie's name and secure prefix, reassembles a chunked one, and decrypts it
 * with AUTH_SECRET.
 *
 * Undefined when there is no usable token: no session, an unconfigured install,
 * or a cookie that will not decrypt (an AUTH_SECRET that has been rotated). Every
 * caller has to handle that anyway, because the far end can refuse the token too.
 *
 * With SSO off there is no token and nothing to present. That is coherent rather
 * than broken: iam is not configured either, and the routes that would want one
 * are not reachable in that mode.
 */
export async function currentOctoToken(): Promise<string | undefined> {
  if (!authEnabled) return undefined;
  const secret = process.env.AUTH_SECRET;
  if (!secret) return undefined;
  try {
    const jwt = await getToken({ req: { headers: await headers() }, secret });
    const token = (jwt as OctoTokenFields | null)?.octoToken;
    return typeof token === "string" && token !== "" ? token : undefined;
  } catch (err) {
    // A cookie that will not decrypt is not an error worth throwing across a
    // server-action boundary: the caller is about to be told they are not
    // authorized, which is the truth of it.
    console.warn(`could not read the platform token from the session: ${(err as Error).message}`);
    return undefined;
  }
}
