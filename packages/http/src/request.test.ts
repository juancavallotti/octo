import { afterEach, describe, expect, it, vi } from "vitest";
import { requestJson } from "./request";

/** A fetch stub returning the given response shape. */
function stubFetch(res: { ok?: boolean; status?: number; body?: unknown }) {
  const fn = vi.fn(async () => ({
    ok: res.ok ?? true,
    status: res.status ?? 200,
    json: async () => res.body,
  })) as unknown as typeof fetch;
  global.fetch = fn;
  return fn;
}

describe("requestJson", () => {
  afterEach(() => vi.restoreAllMocks());

  it("returns ok with the parsed body on success", async () => {
    stubFetch({ body: { id: "1" } });
    const res = await requestJson<{ id: string }>("GET", "http://x/thing");
    expect(res).toEqual({ ok: true, data: { id: "1" } });
  });

  it("JSON-encodes the body and sets the method", async () => {
    const fetchFn = stubFetch({ body: {} });
    await requestJson("POST", "http://x/thing", { a: 1 });
    expect(fetchFn).toHaveBeenCalledWith("http://x/thing", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ a: 1 }),
    });
  });

  it("omits the body for a request without one", async () => {
    const fetchFn = stubFetch({ status: 204 });
    await requestJson("DELETE", "http://x/thing");
    expect(fetchFn).toHaveBeenCalledWith("http://x/thing", { method: "DELETE" });
  });

  it("returns ok with undefined data for 204", async () => {
    stubFetch({ status: 204 });
    const res = await requestJson("DELETE", "http://x/thing");
    expect(res).toEqual({ ok: true, data: undefined });
  });

  it("unwraps the { error } envelope on failure", async () => {
    stubFetch({ ok: false, status: 409, body: { error: "deployed to prod" } });
    const res = await requestJson("DELETE", "http://x/thing");
    expect(res).toEqual({ ok: false, error: "deployed to prod" });
  });

  it("falls back to a status message when there is no error body", async () => {
    stubFetch({ ok: false, status: 500, body: {} });
    const res = await requestJson("GET", "http://x/thing");
    expect(res).toEqual({ ok: false, error: "request failed (500)" });
  });

  it("turns a network error into an error result", async () => {
    global.fetch = vi.fn(async () => {
      throw new Error("ECONNREFUSED");
    }) as unknown as typeof fetch;
    const res = await requestJson("GET", "http://x/thing");
    expect(res).toEqual({ ok: false, error: "request failed: ECONNREFUSED" });
  });
});
