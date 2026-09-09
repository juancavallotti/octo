import { describe, expect, it } from "vitest";
import { sameOrigin } from "./origin";

/**
 * The cases below are the reason this function exists. Every one of the rejected
 * URLs passes a `startsWith(origin)` test, which is what the navigation guard and
 * the IPC check both used before.
 */
const ORIGIN = "http://127.0.0.1:8477";

describe("sameOrigin", () => {
  it("accepts the server's own pages", () => {
    for (const url of [
      "http://127.0.0.1:8477",
      "http://127.0.0.1:8477/",
      "http://127.0.0.1:8477/?file=orders.yaml",
      "http://127.0.0.1:8477/editor/runs/abc/",
      "http://127.0.0.1:8477/mcp",
    ]) {
      expect(sameOrigin(url, ORIGIN), url).toBe(true);
    }
  });

  it("rejects a host smuggled in as userinfo", () => {
    // The whole point: this string starts with the origin, and its host is not ours.
    expect(new URL("http://127.0.0.1:8477@evil.example/x").host).toBe("evil.example");
    expect(sameOrigin("http://127.0.0.1:8477@evil.example/x", ORIGIN)).toBe(false);
    expect(sameOrigin("http://127.0.0.1:8477:pass@evil.example/", ORIGIN)).toBe(false);
  });

  it("rejects a different port that shares our prefix", () => {
    expect(sameOrigin("http://127.0.0.1:84771/x", ORIGIN)).toBe(false);
  });

  it("rejects a different host, scheme, or port", () => {
    for (const url of [
      "http://localhost:8477/",       // same server, different name: not the same origin
      "https://127.0.0.1:8477/",
      "http://127.0.0.1:8478/",
      "http://evil.example/",
    ]) {
      expect(sameOrigin(url, ORIGIN), url).toBe(false);
    }
  });

  it("rejects anything unparseable rather than guessing", () => {
    for (const url of ["", "not a url", "://x", "javascript:alert(1)"]) {
      expect(sameOrigin(url, ORIGIN), url).toBe(false);
    }
  });
});
