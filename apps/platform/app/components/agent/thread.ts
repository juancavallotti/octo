/** Naming a conversation, and remembering which one this tab is in. */

/**
 * A random id, without assuming a secure context.
 *
 * crypto.randomUUID exists only over HTTPS or on localhost, and a self-hosted
 * platform served over plain HTTP is an ordinary way to run this — where calling
 * it throws.
 *
 * These ids key React lists and name a conversation; the conversation is scoped
 * server-side by the authenticated user, so this is not a security boundary and
 * the fallbacks only need to not collide.
 */
export function randomId(): string {
  const c = globalThis.crypto;
  if (typeof c?.randomUUID === "function") return c.randomUUID();
  if (typeof c?.getRandomValues === "function") {
    const bytes = c.getRandomValues(new Uint8Array(16));
    return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
  }
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 12)}`;
}

/** Mint a thread id, keyed per user so signing out cannot resume someone else's. */
export function threadKey(userKey: string): string {
  return `octo.agent.thread.${userKey}`;
}

export function readThreadId(userKey: string): string {
  const key = threadKey(userKey);
  const existing = sessionStorage.getItem(key);
  if (existing) return existing;
  const minted = randomId();
  sessionStorage.setItem(key, minted);
  return minted;
}
