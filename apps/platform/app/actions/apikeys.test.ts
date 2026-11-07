import { beforeEach, describe, expect, it, vi } from "vitest";

const listApiKeys = vi.fn();
vi.mock("./client/apikeys", () => ({
  listApiKeys: (...a: unknown[]) => listApiKeys(...a),
}));
vi.mock("./_auth", () => ({
  withUser: (fn: (userId: string) => unknown) => fn("user-1"),
}));
let environment = "";
vi.mock("@/app/auth/callerEnvironment", () => ({
  callerEnvironment: async () => environment,
}));

import * as actions from "./apikeys";

const keys = [
  { id: "k-default", name: "laptop" },
  { id: "k-stage", name: "ci", environment: "stage" },
  { id: "k-prod", name: "deploy", environment: "prod" },
];

describe("listApiKeys", () => {
  beforeEach(() => {
    listApiKeys.mockResolvedValue({ ok: true, data: keys });
  });

  it("lists the default environment's keys in the default environment", async () => {
    environment = "";
    const res = await actions.listApiKeys();
    expect(res.ok && res.data.map((k) => k.id)).toEqual(["k-default"]);
  });

  it("lists only the keys bound to the environment the caller works in", async () => {
    environment = "stage";
    const res = await actions.listApiKeys();
    expect(res.ok && res.data.map((k) => k.id)).toEqual(["k-stage"]);
  });
});
