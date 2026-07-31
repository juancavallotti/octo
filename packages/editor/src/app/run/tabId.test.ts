import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { runTabId, resetTabIdForTest } from "./tabId";

const STORAGE_KEY = "octo_run_tab";

/** What a host accepts before it will mix the id into a namespace — kept in step
 * with run-host's `isValidTabId`, which this package can't import (it is the
 * server-side half, and the dependency only runs the other way). */
const HOST_ACCEPTS = /^[A-Za-z0-9_-]{8,64}$/;

/** Stands in for another tab of this browser that is still holding `id` — it answers
 * the claim exactly as a live tab's own defender would. */
function holdTabId(id: string): BroadcastChannel {
  const ch = new BroadcastChannel("octo_run_tab");
  ch.onmessage = (ev: MessageEvent) => {
    if (ev.data?.type === "claim" && ev.data.id === id) {
      ch.postMessage({ type: "taken", id });
    }
  };
  return ch;
}

beforeEach(() => {
  resetTabIdForTest();
  window.sessionStorage.clear();
});

/**
 * Models a browser without the secure-context-only API.
 *
 * It has to actually be absent: the code under test checks `typeof === "function"`,
 * so a spy returning undefined would sail straight past. `randomUUID` lives on the
 * prototype here, which makes `delete` a no-op — shadow it with an own property
 * instead, and drop the shadow afterwards to reveal the real one again.
 */
function withoutRandomUUID(): void {
  Object.defineProperty(crypto, "randomUUID", { value: undefined, configurable: true });
}

afterEach(() => {
  vi.restoreAllMocks();
  // crypto is shared by every test in this worker, so the shadow must not outlive
  // the case that set it.
  delete (crypto as { randomUUID?: unknown }).randomUUID;
});

describe("runTabId", () => {
  it("mints an id the host will accept", async () => {
    // The server rejects a malformed tab id and falls back to the plain cookie
    // namespace, which would silently put every tab back on one runner.
    expect(await runTabId()).toMatch(HOST_ACCEPTS);
  });

  it("returns the same id for the life of the tab", async () => {
    expect(await runTabId()).toBe(await runTabId());
  });

  // A reload is a new module instance reading the same sessionStorage — which is
  // what keeps a tab attached to the runner it started.
  it("reuses the id a previous load stored", async () => {
    const first = await runTabId();
    resetTabIdForTest();
    expect(await runTabId()).toBe(first);
    expect(window.sessionStorage.getItem(STORAGE_KEY)).toBe(first);
  });

  it("gives a tab with nothing stored a fresh id", async () => {
    const first = await runTabId();
    resetTabIdForTest();
    window.sessionStorage.clear();
    expect(await runTabId()).not.toBe(first);
  });

  // Duplicating a tab hands the copy its opener's sessionStorage, id and all, so
  // storage alone would put both on one runner — the very thing this exists to stop.
  it("takes a fresh id when a live tab still holds the stored one", async () => {
    const inherited = await runTabId();
    const sibling = holdTabId(inherited);
    resetTabIdForTest(); // the duplicate: same storage, no memoized id

    const id = await runTabId();
    expect(id).not.toBe(inherited);
    expect(window.sessionStorage.getItem(STORAGE_KEY)).toBe(id);

    sibling.close();
  });

  // The other side of the same coin: a plain reload has no live claimant, and must
  // not be handed a new runner just because it asked.
  it("keeps the stored id when no tab answers", async () => {
    const first = await runTabId();
    resetTabIdForTest();

    expect(await runTabId()).toBe(first);
  });

  // randomUUID is secure-context-only, so a host served over plain HTTP does not
  // have it. Minting has to keep working there, or every tab falls back to sharing.
  it("mints without randomUUID, as an insecure context must", async () => {
    withoutRandomUUID();

    expect(await runTabId()).toMatch(HOST_ACCEPTS);
  });

  // The answer is memoized, so a rejection would be memoized too and break every
  // later RUN call for the life of the page. Degrade to "no tab" instead: the host
  // reads that as the plain cookie namespace, i.e. how RUN behaved before tabs.
  it("resolves to no id rather than rejecting when one cannot be made", async () => {
    withoutRandomUUID();
    vi.spyOn(crypto, "getRandomValues").mockImplementation(() => {
      throw new Error("no crypto here");
    });

    await expect(runTabId()).resolves.toBe("");
    await expect(runTabId()).resolves.toBe("");
  });

  // Safari's private mode and sandboxed frames throw on access rather than
  // returning null. Losing storage costs stability across reloads, not isolation.
  it("still yields a usable id when sessionStorage throws", async () => {
    vi.spyOn(window.sessionStorage, "getItem").mockImplementation(() => {
      throw new Error("denied");
    });
    vi.spyOn(window.sessionStorage, "setItem").mockImplementation(() => {
      throw new Error("denied");
    });

    const id = await runTabId();
    expect(id).toMatch(HOST_ACCEPTS);
    expect(await runTabId()).toBe(id);
  });
});
