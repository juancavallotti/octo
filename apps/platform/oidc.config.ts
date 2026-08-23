/**
 * The platform's OIDC provider configuration, read from the environment.
 *
 * Octo does not ship an identity provider and does not privilege one: any
 * standards-compliant OIDC provider works. Everything an operator can say about
 * theirs is an `OIDC_*` env var, and it is all read here — once — so the editor's
 * Auth.js config (auth.config.ts) and the `/mcp` resource server
 * (app/mcp/oauth-config.ts) cannot drift apart on which issuer they trust.
 *
 * Only `OIDC_ISSUER` / `OIDC_CLIENT_ID` / `OIDC_CLIENT_SECRET` are required;
 * everything else has a working default. Leaving the issuer unset disables SSO
 * entirely, which is what local `task dev` runs do (see `authEnabled`).
 *
 * This module is deliberately dependency-free: it is imported by the edge-safe
 * auth config and by the lightweight `.well-known` metadata routes, neither of
 * which can afford to pull in `jose` or the orchestrator client.
 */

/** Trim any trailing slashes so we can safely append a path. */
export function trimSlashes(value: string): string {
  return value.replace(/\/+$/, "");
}

/** Read an env var, returning undefined rather than "" when it is unset/blank. */
function env(name: string): string | undefined {
  const value = process.env[name];
  return value && value !== "" ? value : undefined;
}

/**
 * The Auth.js provider id, and with it the callback path
 * `{AUTH_URL}/api/auth/callback/oidc` that must be registered with the provider.
 *
 * Deliberately a constant and not a knob: it is not user-visible (that is
 * {@link OIDC_PROVIDER_NAME}), and making it configurable would only add a way
 * to silently invalidate the registered redirect URI.
 */
export const OIDC_PROVIDER_ID = "oidc";

/**
 * The provider's issuer URL — the base its discovery document hangs off, and the
 * exact string tokens must carry as `iss`. Empty when SSO is unconfigured.
 *
 * Kept byte-for-byte as configured, trailing slash and all: the issuer is an
 * identifier, not a path, and some providers' really does end in one (Auth0's
 * `iss` is `https://tenant.auth0.com/`). Normalizing it here would silently fail
 * every audience/issuer check against those. Trimming belongs at the call sites
 * that append a path — see {@link trimSlashes}.
 */
export const OIDC_ISSUER = process.env.OIDC_ISSUER ?? "";

/** Client credentials for the authorization-code flow. */
export const OIDC_CLIENT_ID = env("OIDC_CLIENT_ID");
export const OIDC_CLIENT_SECRET = env("OIDC_CLIENT_SECRET");

/**
 * Space-separated scopes requested at sign-in. The default asks for the profile
 * and email scopes so we receive the user's name and picture; providers that
 * carve roles into a separate scope (or that reject one of these) can say so.
 */
export const OIDC_SCOPES = env("OIDC_SCOPES") ?? "openid profile email";

/** How the provider is named on the sign-in button ("Sign in with …"). */
export const OIDC_PROVIDER_NAME = env("OIDC_PROVIDER_NAME") ?? "OIDC";

/**
 * Endpoint overrides, for providers whose discovery document is unreachable,
 * incomplete, or wrong. Each is undefined by default, which leaves Auth.js (and
 * the /mcp verifier) to discover it from the issuer — the path every compliant
 * provider takes.
 */
export const OIDC_AUTHORIZATION_URL = env("OIDC_AUTHORIZATION_URL");
export const OIDC_TOKEN_URL = env("OIDC_TOKEN_URL");
export const OIDC_USERINFO_URL = env("OIDC_USERINFO_URL");
export const OIDC_JWKS_URL = env("OIDC_JWKS_URL");

/** The issuer's origin, or undefined if the issuer is unset or unparseable. */
function issuerOrigin(): string | undefined {
  if (!OIDC_ISSUER) return undefined;
  try {
    return new URL(OIDC_ISSUER).origin;
  } catch {
    // A malformed issuer is a configuration error, but it is auth.config.ts's to
    // report at sign-in — not this module's to throw at import time.
    return undefined;
  }
}

/**
 * Logo shown next to the sign-in button. Defaults to the issuer's favicon, which
 * is right often enough to be worth trying and costs nothing when it 404s — the
 * button renders without it (see app/(public)/ProviderLogo.tsx).
 */
export const OIDC_PROVIDER_LOGO = (() => {
  const explicit = env("OIDC_PROVIDER_LOGO");
  if (explicit) return explicit;
  const origin = issuerOrigin();
  return origin ? `${origin}/favicon.ico` : undefined;
})();
