import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";

// The model now delegates to server actions, which authorize via the auth guard
// (`@/auth`). next-auth can't load in the vitest environment, so stub the guard's
// dependency: auth disabled → every action authorizes with the local session.
vi.mock("@/auth", () => ({
  authEnabled: false,
  auth: async () => null,
}));

import {
  assignIntegration,
  createIntegration,
  deleteIntegration,
  listFolders,
  listIntegrations,
  updateIntegration,
} from "./orchestrator";
import { deleteSecret, listSecrets, setSecret } from "./secrets";

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

// All actions call the orchestrator directly at ORCHESTRATOR_URL.
const ORCH = "http://orchestrator.test";

beforeAll(() => {
  process.env.ORCHESTRATOR_URL = ORCH;
});

describe("orchestrator client", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("lists integrations from the orchestrator", async () => {
    const fetchFn = stubFetch({ body: [{ id: "1", name: "a" }] });
    const out = await listIntegrations();
    expect(out).toEqual([{ id: "1", name: "a" }]);
    expect(fetchFn).toHaveBeenCalledWith(`${ORCH}/integrations`, {
      method: "GET",
    });
  });

  it("posts a JSON body when creating", async () => {
    const fetchFn = stubFetch({ status: 201, body: { id: "1" } });
    await createIntegration({ name: "n", definition: "yaml" });
    const [url, init] = (fetchFn as unknown as ReturnType<typeof vi.fn>).mock
      .calls[0];
    expect(url).toBe(`${ORCH}/integrations`);
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
    expect(url).toBe(`${ORCH}/integrations/a%2Fb`);
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
      `${ORCH}/folders/f1/integrations/i1`,
      expect.objectContaining({ method: "PUT" }),
    );
  });

  it("unwraps the { error } envelope on failure", async () => {
    stubFetch({ ok: false, status: 400, body: { error: "bad name" } });
    await expect(listFolders()).rejects.toThrow("bad name");
  });

  // --- Secrets --------------------------------------------------------------

  it("lists secrets from the orchestrator", async () => {
    const fetchFn = stubFetch({ body: [{ name: "API_KEY" }] });
    const out = await listSecrets();
    expect(out).toEqual([{ name: "API_KEY" }]);
    expect(fetchFn).toHaveBeenCalledWith(`${ORCH}/secrets`, { method: "GET" });
  });

  it("sets a secret via PUT with a value body and encoded name", async () => {
    const fetchFn = stubFetch({ body: { name: "API_KEY" } });
    await setSecret("API_KEY", "shh");
    const [url, init] = (fetchFn as unknown as ReturnType<typeof vi.fn>).mock
      .calls[0];
    expect(url).toBe(`${ORCH}/secrets/API_KEY`);
    expect(init).toMatchObject({
      method: "PUT",
      body: JSON.stringify({ value: "shh" }),
    });
  });

  it("deletes a secret, passing force as a query param", async () => {
    const fetchFn = stubFetch({ status: 204 });
    await deleteSecret("API_KEY", true);
    expect(fetchFn).toHaveBeenCalledWith(
      `${ORCH}/secrets/API_KEY?force=true`,
      expect.objectContaining({ method: "DELETE" }),
    );
  });

  it("deletes a secret without force by default", async () => {
    const fetchFn = stubFetch({ status: 204 });
    await deleteSecret("API_KEY");
    expect(fetchFn).toHaveBeenCalledWith(
      `${ORCH}/secrets/API_KEY`,
      expect.objectContaining({ method: "DELETE" }),
    );
  });
});
