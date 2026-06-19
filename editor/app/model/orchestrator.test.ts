import { afterEach, describe, expect, it, vi } from "vitest";
import {
  assignIntegration,
  createIntegration,
  deleteIntegration,
  listFolders,
  listIntegrations,
  updateIntegration,
} from "./orchestrator";

/** Build a fetch stub that records its calls and returns the given response. */
function stubFetch(res: {
  ok?: boolean;
  status?: number;
  body?: unknown;
}): typeof fetch {
  const fn = vi.fn(async () => ({
    ok: res.ok ?? true,
    status: res.status ?? 200,
    json: async () => res.body,
  })) as unknown as typeof fetch;
  global.fetch = fn;
  return fn;
}

describe("orchestrator client", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("lists integrations from the BFF route", async () => {
    const fetchFn = stubFetch({ body: [{ id: "1", name: "a" }] });
    const out = await listIntegrations();
    expect(out).toEqual([{ id: "1", name: "a" }]);
    expect(fetchFn).toHaveBeenCalledWith("/api/integrations", undefined);
  });

  it("posts a JSON body when creating", async () => {
    const fetchFn = stubFetch({ status: 201, body: { id: "1" } });
    await createIntegration({ name: "n", definition: "yaml" });
    const [url, init] = (fetchFn as unknown as ReturnType<typeof vi.fn>).mock
      .calls[0];
    expect(url).toBe("/api/integrations");
    expect(init).toMatchObject({
      method: "POST",
      body: JSON.stringify({ name: "n", definition: "yaml" }),
    });
  });

  it("uses PUT when updating and encodes the id", async () => {
    const fetchFn = stubFetch({ body: { id: "a/b" } });
    await updateIntegration("a/b", { name: "n", definition: "y" });
    const [url, init] = (fetchFn as unknown as ReturnType<typeof vi.fn>).mock
      .calls[0];
    expect(url).toBe("/api/integrations/a%2Fb");
    expect(init).toMatchObject({ method: "PUT" });
  });

  it("returns void for 204 responses", async () => {
    stubFetch({ status: 204, body: undefined });
    await expect(deleteIntegration("1")).resolves.toBeUndefined();
  });

  it("assigns an integration to a folder via PUT", async () => {
    const fetchFn = stubFetch({ status: 204 });
    await assignIntegration("f1", "i1");
    expect(fetchFn).toHaveBeenCalledWith(
      "/api/folders/f1/integrations/i1",
      expect.objectContaining({ method: "PUT" }),
    );
  });

  it("unwraps the { error } envelope on failure", async () => {
    stubFetch({ ok: false, status: 400, body: { error: "bad name" } });
    await expect(listFolders()).rejects.toThrow("bad name");
  });
});
