/**
 * The platform token's life inside a session: obtained at sign-in, kept fresh
 * while the session lasts, and given up when it can no longer be renewed.
 *
 * It lives here rather than in the Auth.js callbacks for two reasons. The
 * callbacks are three lines of plumbing and this is the policy; and this is
 * testable without Auth.js, which matters because both behaviours it depends on
 * are internal to a beta release.
 *
 * ## Where the token is, and where it is not
 *
 * The token is written onto the JWT — the payload of the encrypted session cookie
 * — and deliberately NOT onto the `session` object. Auth.js serializes `session`
 * to the browser at /api/auth/session, so anything put there is readable by client
 * JavaScript, and a bearer that reaches the browser is a bearer that can call the
 * platform's API directly, around the very boundary the server actions exist to
 * be. Roles go on the session (they are for rendering); the credential does not.
 *
 * ## Why the proxy is what actually refreshes it
 *
 * `auth()` called with no arguments — in a server component or a server action —
 * discards the response's Set-Cookie headers (next-auth/lib/index.js: the no-args
 * branch reads `.json()` and drops the rest). Only the `auth((req) => …)` wrapper
 * copies them onto the outgoing response, and the one place that uses it is
 * proxy.ts. So a re-mint during a render is used for that render and then thrown
 * away, and the write that sticks is the one that happens on a request passing
 * through the proxy. Its matcher covers every navigation and every server-action
 * POST, so in practice the cookie is rewritten promptly; a tab left idle for an
 * hour simply refreshes on its next request.
 *
 * That is also why re-mints are single-flighted below: a render calling `auth()`
 * several times would otherwise ask iam several times for a token it is about to
 * discard.
 */

import { exchangeIdToken, refreshOctoToken, type PlatformToken } from "@/app/actions/client/iam";

/**
 * How long before expiry a token is renewed. Five minutes against a token that
 * lives an hour: far enough ahead that no request goes out holding something
 * about to die, short enough that the renewal is rare.
 */
const REMINT_LEAD_MS = 5 * 60 * 1000;

/** What a session carries about its platform token. */
export interface OctoTokenFields {
  octoToken?: string;
  octoExpiresAt?: number;
  roles?: string[];
  userId?: string;
}

/**
 * In-flight re-mints, keyed on the token being replaced. Entries are deleted when
 * they settle, so this holds at most one promise per session that is currently
 * renewing — it is a coalescing window, not a cache.
 */
const inFlight = new Map<string, Promise<PlatformToken | "expired" | "unavailable">>();

/**
 * Exchange the identity provider's id token for a platform token, at sign-in.
 *
 * Returns null when the exchange fails, and the caller turns that into a refused
 * sign-in. Refusing is the point: a session with no platform token looks signed in
 * and can call nothing, which is a far worse thing to hand somebody than an error
 * at the door.
 */
export async function signInExchange(idToken: string): Promise<OctoTokenFields | null> {
  const res = await exchangeIdToken(idToken);
  if (!res.ok) {
    console.error(`sign-in could not obtain a platform token from iam: ${res.error}`);
    return null;
  }
  return fieldsFrom(res.data);
}

/**
 * Renew the token if it is close to expiry, and report what to do about it.
 *
 * The three outcomes are the whole policy, and the third is the one worth being
 * careful about:
 *
 *  - renewed, or not yet due → the fields to carry on with;
 *  - iam refused the token (401) → null, and the caller ends the session. The
 *    token is past the grace window, or the user it spoke for is gone;
 *  - iam could not be reached, or is unconfigured → the existing fields, unchanged.
 *    A restarting iam must not sign out everybody who is signed in. The token
 *    stays valid for as long as it already was, and the next request tries again.
 */
export async function keepFresh(fields: OctoTokenFields): Promise<OctoTokenFields | null> {
  const { octoToken, octoExpiresAt } = fields;
  if (!octoToken || !octoExpiresAt) {
    // No token to keep fresh. Either this session predates the exchange or its
    // sign-in was refused; both mean it cannot be used and should end.
    return null;
  }
  if (octoExpiresAt - Date.now() > REMINT_LEAD_MS) return fields;

  const result = await coalesced(octoToken);
  if (result === "expired") return null;
  if (result === "unavailable") return fields;
  return fieldsFrom(result);
}

/** Run at most one re-mint per token at a time; see the note at the top. */
function coalesced(octoToken: string): Promise<PlatformToken | "expired" | "unavailable"> {
  const existing = inFlight.get(octoToken);
  if (existing) return existing;

  const attempt = refreshOctoToken(octoToken)
    .then((res): PlatformToken | "expired" | "unavailable" => {
      if (res.ok) return res.data;
      if (res.status === 401) return "expired";
      // Anything else is iam's problem rather than this token's: a 503 while it
      // starts, a network error, an unset IAM_URL.
      console.warn(`could not renew the platform token, keeping the current one: ${res.error}`);
      return "unavailable";
    })
    .finally(() => inFlight.delete(octoToken));

  inFlight.set(octoToken, attempt);
  return attempt;
}

/** The session fields a minted token produces. */
function fieldsFrom(minted: PlatformToken): OctoTokenFields {
  return {
    octoToken: minted.token,
    octoExpiresAt: new Date(minted.expiresAt).getTime(),
    roles: minted.user.roles,
    userId: minted.user.id,
  };
}
