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
 * `getToken` reassembles a chunked cookie and decrypts it with AUTH_SECRET. It
 * will not work out the cookie's name on its own, though — see
 * {@link secureCookies} — so it is told.
 *
 * Undefined when there is no usable token: no session, or a cookie that will not
 * decrypt.
 */
export async function currentOctoToken(): Promise<string | undefined> {
  const secret = process.env.AUTH_SECRET;
  if (!secret) return undefined;
  try {
    const incoming = await headers();
    const jwt = await getToken({
      req: { headers: incoming },
      secret,
      secureCookie: secureCookies(incoming),
    });
    const token = (jwt as OctoTokenFields | null)?.octoToken;
    return typeof token === "string" && token !== "" ? token : undefined;
  } catch (err) {
    // A cookie that will not decrypt is "no token", not a failure: the caller
    // handles undefined already.
    console.warn(`could not read the platform token from the session: ${(err as Error).message}`);
    return undefined;
  }
}

/**
 * Whether Auth.js prefixed this install's session cookie with `__Secure-`.
 *
 * Auth.js decides that from the protocol of the URL the request came in on, and
 * builds that URL from AUTH_URL when it is set, falling back to the forwarded
 * protocol and finally assuming https (@auth/core's `createActionURL`). This is
 * the same rule, read from the same places, because the two have to agree.
 *
 * Getting it wrong is silent and total: `getToken` derives the decryption salt
 * from the cookie's name as well, so a mismatched prefix is a token that is
 * neither found nor decryptable, and that reads as "not signed in" rather than as
 * an error.
 *
 * It cannot be left to `getToken`, which defaults the flag to false. An install
 * served over https would then always look for the wrong cookie while every local
 * run looked for the right one — a bug that only ever appears in production.
 */
function secureCookies(incoming: Headers): boolean {
  const configured = process.env.AUTH_URL ?? process.env.NEXTAUTH_URL;
  if (configured) {
    try {
      return new URL(configured).protocol === "https:";
    } catch {
      // Unparseable, so it says nothing about the protocol. Auth.js swallows this
      // too, and both of us then fall back to the request.
    }
  }
  const forwarded = incoming.get("x-forwarded-proto") ?? "https";
  return forwarded.replace(/:$/, "") === "https";
}
