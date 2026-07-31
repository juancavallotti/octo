// @vitest-environment node
import { describe, expect, it } from "vitest";
import { deriveNamespace, isValidNamespace, isValidTabId, newNamespace } from "./namespace";

const BASE = "aaaa0000";
const TAB = "3f9c1e7b8a4d2c5e6f0b1a9d8c7e6f5a";

describe("isValidTabId", () => {
  it("accepts the hex a browser mints from randomUUID", () => {
    expect(isValidTabId(TAB)).toBe(true);
  });

  it("accepts url-safe punctuation within the length bounds", () => {
    expect(isValidTabId("abc-123_def")).toBe(true);
  });

  it("rejects ids outside the length bounds", () => {
    expect(isValidTabId("short")).toBe(false);
    expect(isValidTabId("x".repeat(65))).toBe(false);
  });

  // The tab id is hashed, never used as a path — but bounding its shape keeps a
  // garbage value a rejection rather than a silently-accepted namespace.
  it("rejects path and whitespace characters", () => {
    expect(isValidTabId("../../etc/passwd")).toBe(false);
    expect(isValidTabId("has spaces here")).toBe(false);
  });
});

describe("deriveNamespace", () => {
  it("is stable for the same browser and tab", () => {
    // This is what lets a tab keep its runner across a reload.
    expect(deriveNamespace(BASE, TAB)).toBe(deriveNamespace(BASE, TAB));
  });

  it("gives two tabs of one browser different namespaces", () => {
    const a = deriveNamespace(BASE, "1111111111111111");
    const b = deriveNamespace(BASE, "2222222222222222");
    expect(a).not.toBe(b);
  });

  // The whole point of keying the HMAC with the cookie: a tab id is client-supplied,
  // so if it alone decided the namespace, any user could name another user's runner.
  it("gives two browsers different namespaces even for an identical tab id", () => {
    expect(deriveNamespace("aaaa0000", TAB)).not.toBe(deriveNamespace("bbbb1111", TAB));
  });

  it("never collides with the cookie namespace it was derived from", () => {
    expect(deriveNamespace(BASE, TAB)).not.toBe(BASE);
  });

  it("always produces a valid slug", () => {
    for (let i = 0; i < 500; i++) {
      const ns = deriveNamespace(newNamespace(), `tab-${i}-${"0".repeat(8)}`);
      expect(isValidNamespace(ns)).toBe(true);
    }
  });

  // The single fallback seam: callers hand this a raw untrusted value, and anything
  // unusable means "this client has no tab" — i.e. behave exactly as before tabs existed.
  it("falls back to the cookie namespace when the tab id is missing or malformed", () => {
    expect(deriveNamespace(BASE, null)).toBe(BASE);
    expect(deriveNamespace(BASE, undefined)).toBe(BASE);
    expect(deriveNamespace(BASE, "")).toBe(BASE);
    expect(deriveNamespace(BASE, "short")).toBe(BASE);
    expect(deriveNamespace(BASE, "x".repeat(65))).toBe(BASE);
    expect(deriveNamespace(BASE, "../etc/passwd")).toBe(BASE);
  });
});
