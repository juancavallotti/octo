/**
 * The id generator's fallbacks, which is the part of this that does not run on a
 * developer's laptop.
 *
 * crypto.randomUUID exists only over HTTPS or on localhost, and a self-hosted
 * platform served over plain HTTP is an ordinary way to run this — where calling
 * it threw synchronously out of send(), before the try, leaving the chat wedged
 * for the life of the page. These are the paths that were reached instead.
 */

import { afterEach, describe, expect, it, vi } from "vitest";

import { randomId, readThreadId, threadKey } from "./thread";

afterEach(() => {
  vi.unstubAllGlobals();
  sessionStorage.clear();
});

describe("randomId", () => {
  it("uses randomUUID where there is one", () => {
    vi.stubGlobal("crypto", { randomUUID: () => "from-uuid" });
    expect(randomId()).toBe("from-uuid");
  });

  it("falls back to random bytes without randomUUID", () => {
    vi.stubGlobal("crypto", {
      getRandomValues: (a: Uint8Array) => a.fill(0xab),
    });
    expect(randomId()).toBe("ab".repeat(16));
  });

  // No crypto at all is the case that mattered: these ids key React lists and
  // name a conversation the server scopes anyway, so they only have to not
  // collide.
  it("still produces something with no crypto at all", () => {
    vi.stubGlobal("crypto", undefined);
    expect(randomId()).not.toBe(randomId());
    expect(randomId().length).toBeGreaterThan(8);
  });
});

describe("readThreadId", () => {
  it("mints one and keeps it for the session", () => {
    const first = readThreadId("u-1");
    expect(readThreadId("u-1")).toBe(first);
    expect(sessionStorage.getItem(threadKey("u-1"))).toBe(first);
  });

  // Keyed per user, so signing out and back in as somebody else cannot resume
  // their conversation.
  it("gives another user another conversation", () => {
    expect(readThreadId("u-1")).not.toBe(readThreadId("u-2"));
  });
});
