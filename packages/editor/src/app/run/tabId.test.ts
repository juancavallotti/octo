import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { runTabId, resetTabIdForTest } from "./tabId";

const STORAGE_KEY = "octo_run_tab";

/** What a host accepts before it will mix the id into a namespace — kept in step
 * with run-host's `isValidTabId`, which this package can't import (it is the
 * server-side half, and the dependency only runs the other way). */
const HOST_ACCEPTS = /^[A-Za-z0-9_-]{8,64}$/;

beforeEach(() => {
  resetTabIdForTest();
  window.sessionStorage.clear();
});

afterEach(() => {
  vi.restoreAllMocks();
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
