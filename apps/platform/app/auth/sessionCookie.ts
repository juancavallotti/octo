/**
 * Reading the platform token back out of the session cookie.
 *
 * The only module here that touches Auth.js's cookie internals, which is why it
 * is separate: importing it pulls in `next/server`.
 */

import { headers } from "next/headers";
import { getToken } from "next-auth/jwt";
import type { OctoTokenFields } from "./octoToken";

/**
 * The caller's platform token, for a server-side call that has to present it.
 *
 * `getToken` works out the cookie's name and secure prefix, reassembles a chunked
 * one, and decrypts it with AUTH_SECRET.
 *
 * Undefined when there is no usable token: no session, or a cookie that will not
 * decrypt.
 */
export async function currentOctoToken(): Promise<string | undefined> {
  const secret = process.env.AUTH_SECRET;
  if (!secret) return undefined;
  try {
    const jwt = await getToken({ req: { headers: await headers() }, secret });
    const token = (jwt as OctoTokenFields | null)?.octoToken;
    return typeof token === "string" && token !== "" ? token : undefined;
  } catch (err) {
    // A cookie that will not decrypt is "no token", not a failure: the caller
    // handles undefined already.
    console.warn(`could not read the platform token from the session: ${(err as Error).message}`);
    return undefined;
  }
}
