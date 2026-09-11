import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const { requestJson, callerToken } = vi.hoisted(() => ({
  requestJson: vi.fn(),
  callerToken: vi.fn(),
}));

vi.mock("@octo/http", () => ({ requestJson }));
vi.mock("@/app/auth/callerToken", () => ({ callerToken }));

import { observabilityCall } from "./_observability";

const BASE = "http://observability:8091";

describe("the observability transport", () => {
  beforeEach(() => {
    process.env.OBSERVABILITY_URL = BASE;
    requestJson.mockResolvedValue({ ok: true, data: {} });
  });

  afterEach(() => {
    vi.clearAllMocks();
    delete process.env.OBSERVABILITY_URL;
  });

  // The whole reason this module exists: one place attaches the credential, so
  // a client cannot forget to.
  it("presents the caller's token", async () => {
    callerToken.mockResolvedValue("a.b.c");

    await observabilityCall("trace query", "GET", "/traces");

    expect(requestJson).toHaveBeenCalledWith("GET", `${BASE}/traces`, undefined, {
      headers: { Authorization: "Bearer a.b.c" },
    });
  });

  // No token is not an error here. An install that is not enforcing serves the
  // call happily, and one that is will say so itself.
  it("calls out without options when there is no token", async () => {
    callerToken.mockResolvedValue(undefined);

    await observabilityCall("trace query", "GET", "/traces");

    expect(requestJson).toHaveBeenCalledWith("GET", `${BASE}/traces`, undefined, undefined);
  });

  it("trims a trailing slash off the address", async () => {
    process.env.OBSERVABILITY_URL = `${BASE}/`;
    callerToken.mockResolvedValue(undefined);

    await observabilityCall("retention", "POST", "/retention/run");

    expect(requestJson).toHaveBeenCalledWith(
      "POST",
      `${BASE}/retention/run`,
      undefined,
      undefined,
    );
  });

  // The error names the variable and the feature, so a page can say what to set
  // rather than reporting a fetch against "" as an outage.
  it("says what to set when the address is unset, and calls nothing", async () => {
    delete process.env.OBSERVABILITY_URL;

    const res = await observabilityCall("pod stats", "GET", "/stats/d1/pods");

    expect(res.ok).toBe(false);
    if (res.ok) return;
    expect(res.error).toBe("pod stats not configured (OBSERVABILITY_URL unset)");
    expect(requestJson).not.toHaveBeenCalled();
  });
});
