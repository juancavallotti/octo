import { afterEach, describe, expect, it, vi } from "vitest";
import { requestBytes, requestJson, requestOk, sendBytes } from "./request";

/** A fetch stub returning the given response shape. `json` rejects when `body` is
 * the sentinel NON_JSON, to model a non-JSON (e.g. plain-text) response. */
const NON_JSON = Symbol("non-json");
function stubFetch(res: { ok?: boolean; status?: number; body?: unknown }) {
  const fn = vi.fn(async () => ({
    ok: res.ok ?? true,
    status: res.status ?? 200,
    json: async () => {
      if (res.body === NON_JSON) throw new SyntaxError("Unexpected token 'o'");
      return res.body;
    },
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

  // res.json() can resolve to null — or a scalar, or an array — on a failure body. Reading
  // .error off that would throw a TypeError, and this module promises never to throw.
  it("does not throw on a null or non-object failure body", async () => {
    stubFetch({ ok: false, status: 502, body: null });
    await expect(requestJson("GET", "http://x/thing")).resolves.toEqual({
      ok: false,
      error: "request failed (502)",
    });

    stubFetch({ ok: false, status: 500, body: "plain text error" });
    await expect(requestJson("GET", "http://x/thing")).resolves.toEqual({
      ok: false,
      error: "request failed (500)",
    });
  });

  it("turns a network error into an error result", async () => {
    global.fetch = vi.fn(async () => {
      throw new Error("ECONNREFUSED");
    }) as unknown as typeof fetch;
    const res = await requestJson("GET", "http://x/thing");
    expect(res).toEqual({ ok: false, error: "request failed: ECONNREFUSED" });
  });

  it("returns an error result (does not throw) on a non-JSON 2xx body", async () => {
    stubFetch({ status: 200, body: NON_JSON });
    const res = await requestJson("GET", "http://x/thing");
    expect(res).toEqual({ ok: false, error: "invalid JSON response (200)" });
  });
});

describe("requestOk", () => {
  afterEach(() => vi.restoreAllMocks());

  it("reports true for a 2xx (even a non-JSON body) without parsing it", async () => {
    const fetchFn = stubFetch({ status: 200, body: NON_JSON });
    await expect(requestOk("GET", "http://x/healthz")).resolves.toBe(true);
    expect(fetchFn).toHaveBeenCalledWith("http://x/healthz", { method: "GET" });
  });

  it("reports false for a non-2xx", async () => {
    stubFetch({ ok: false, status: 503 });
    await expect(requestOk("GET", "http://x/healthz")).resolves.toBe(false);
  });

  it("reports false on a network error", async () => {
    global.fetch = vi.fn(async () => {
      throw new Error("ECONNREFUSED");
    }) as unknown as typeof fetch;
    await expect(requestOk("GET", "http://x/healthz")).resolves.toBe(false);
  });
});

describe("requestBytes", () => {
  afterEach(() => vi.restoreAllMocks());

  /** A fetch stub whose response body is bytes rather than JSON. */
  function stubBytes(res: {
    ok?: boolean;
    status?: number;
    bytes?: Uint8Array;
    errorBody?: unknown;
  }) {
    const fn = vi.fn(async () => ({
      ok: res.ok ?? true,
      status: res.status ?? 200,
      arrayBuffer: async () => (res.bytes ?? new Uint8Array()).buffer,
      json: async () => res.errorBody ?? null,
    })) as unknown as typeof fetch;
    global.fetch = fn;
    return fn;
  }

  it("returns the body as bytes", async () => {
    stubBytes({ bytes: new Uint8Array([80, 75, 3, 4]) });
    const res = await requestBytes("GET", "http://x/bundle");
    expect(res).toEqual({ ok: true, data: new Uint8Array([80, 75, 3, 4]) });
  });

  it("unwraps the { error } envelope on failure", async () => {
    stubBytes({ ok: false, status: 404, errorBody: { error: "integration not found" } });
    const res = await requestBytes("GET", "http://x/bundle");
    expect(res).toEqual({ ok: false, error: "integration not found" });
  });

  it("turns a network error into an error result", async () => {
    global.fetch = vi.fn(async () => {
      throw new Error("ECONNREFUSED");
    }) as unknown as typeof fetch;
    await expect(requestBytes("GET", "http://x/bundle")).resolves.toEqual({
      ok: false,
      error: "request failed: ECONNREFUSED",
    });
  });
});

describe("sendBytes", () => {
  afterEach(() => vi.restoreAllMocks());

  it("sends the bytes under the given content type and parses the JSON reply", async () => {
    const fetchFn = stubFetch({ status: 201, body: { id: "i1" } });
    const res = await sendBytes<{ id: string }>(
      "POST",
      "http://x/bundle",
      new Uint8Array([1, 2, 3]),
      "application/zip",
    );
    expect(res).toEqual({ ok: true, data: { id: "i1" } });
    const [, init] = (fetchFn as unknown as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(init.method).toBe("POST");
    expect(init.headers).toEqual({ "Content-Type": "application/zip" });
    expect(new Uint8Array(init.body)).toEqual(new Uint8Array([1, 2, 3]));
  });

  // Node's Buffer is a view over a larger pooled ArrayBuffer; sending that buffer
  // whole would leak whatever else happens to be pooled beside it.
  it("sends only the caller's bytes when handed a view over a larger buffer", async () => {
    const fetchFn = stubFetch({ status: 201, body: {} });
    const pooled = new Uint8Array([9, 9, 1, 2, 3, 9]).subarray(2, 5);
    await sendBytes("POST", "http://x/bundle", pooled, "application/zip");
    const [, init] = (fetchFn as unknown as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(new Uint8Array(init.body)).toEqual(new Uint8Array([1, 2, 3]));
  });

  it("unwraps the { error } envelope on failure", async () => {
    stubFetch({ ok: false, status: 400, body: { error: "bundle invalid" } });
    const res = await sendBytes("POST", "http://x/bundle", new Uint8Array(), "application/zip");
    expect(res).toEqual({ ok: false, error: "bundle invalid" });
  });
});
