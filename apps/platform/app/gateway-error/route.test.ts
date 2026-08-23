import { describe, expect, it } from "vitest";
import { GET, POST } from "./route";
import type { NextRequest } from "next/server";

/** A request carrying only the fields the handler reads. */
function req(
  method: string,
  headers: Record<string, string> = {},
  url = "https://notathing.apps.example.com/",
): NextRequest {
  return {
    method,
    headers: new Headers(headers),
    nextUrl: new URL(url),
  } as unknown as NextRequest;
}

describe("gateway-error route", () => {
  it("answers a browser navigation with the page", async () => {
    const res = GET(req("GET", { accept: "text/html,*/*" }));
    expect(res.status).toBe(404);
    expect(res.headers.get("content-type")).toMatch(/text\/html/);
  });

  /**
   * The case a real cluster surfaced: curl and most HTTP libraries send an
   * accept-anything header, so a webhook POST to a dead endpoint used to get 12 KB
   * of markup on every retry. A person arrives by navigation, which is a GET.
   */
  it("answers a webhook POST with JSON even when it accepts anything", async () => {
    const res = POST(req("POST", { accept: "*/*" }));
    expect(res.status).toBe(404);
    expect(res.headers.get("content-type")).toMatch(/application\/json/);
  });

  it("still honours an explicit ask for HTML on a POST", async () => {
    const res = POST(req("POST", { accept: "text/html" }));
    expect(res.headers.get("content-type")).toMatch(/text\/html/);
  });

  it("echoes the controller's status rather than always saying 404", async () => {
    expect(GET(req("GET", { "x-code": "503", accept: "text/html" })).status).toBe(503);
    // A status the gateway does not produce is not ours to speak for.
    expect(GET(req("GET", { "x-code": "418", accept: "text/html" })).status).toBe(404);
  });

  it("never caches: the same address answers differently once something deploys", async () => {
    expect(GET(req("GET", { accept: "text/html" })).headers.get("cache-control")).toBe(
      "no-store",
    );
  });
});
