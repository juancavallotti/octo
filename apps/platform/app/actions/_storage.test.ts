import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const { requestJson } = vi.hoisted(() => ({ requestJson: vi.fn() }));

vi.mock("@octo/http", () => ({ requestJson }));

import * as storage from "./_storage";

const BASE = "http://observability:8091";

describe("the storage-report client", () => {
  beforeEach(() => {
    process.env.OBSERVABILITY_URL = BASE;
  });

  afterEach(() => {
    vi.clearAllMocks();
    delete process.env.OBSERVABILITY_URL;
  });

  it("reads the report from the observability service", async () => {
    const report = { redis: null, database: null, redisReason: "none", databaseReason: "none" };
    requestJson.mockResolvedValue({ ok: true, data: report });

    const res = await storage.getStorageStats();
    expect(res).toEqual({ ok: true, data: report });
    expect(requestJson).toHaveBeenCalledWith("GET", `${BASE}/settings/storage`);
  });

  it("does not double the slash when the address has a trailing one", async () => {
    process.env.OBSERVABILITY_URL = `${BASE}/`;
    requestJson.mockResolvedValue({ ok: true, data: {} });

    await storage.getStorageStats();
    expect(requestJson).toHaveBeenCalledWith("GET", `${BASE}/settings/storage`);
  });

  it("says what to set when the service is unconfigured", async () => {
    delete process.env.OBSERVABILITY_URL;

    const res = await storage.getStorageStats();
    expect(res.ok).toBe(false);
    if (res.ok) return;
    expect(res.error).toMatch(/OBSERVABILITY_URL/);
    expect(requestJson).not.toHaveBeenCalled();
  });
});
