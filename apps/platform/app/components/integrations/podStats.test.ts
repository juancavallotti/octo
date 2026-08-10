import { describe, expect, it } from "vitest";

import { bareHost, totalRestarts } from "./podStats";

import type { Deployment } from "@/app/model/orchestrator";

const pod = (restarts: number) => ({
  name: `p-${restarts}`,
  phase: "Running",
  ready: true,
  restarts,
});

const withPods = (pods: Deployment["pods"]) => ({ pods }) as Deployment;

describe("totalRestarts", () => {
  it("sums across the pods", () => {
    expect(totalRestarts(withPods([pod(1), pod(2), pod(0)]))).toBe(3);
  });

  // A deployment reports no pods before its first status arrives. Zero is the
  // honest answer; the alternative is a crash on the first render of every new
  // deployment.
  it("is zero when there are no pods yet", () => {
    expect(totalRestarts(withPods([]))).toBe(0);
    expect(totalRestarts({} as Deployment)).toBe(0);
  });
});

describe("bareHost", () => {
  it("strips the scheme and keeps everything else", () => {
    expect(bareHost("https://orders.example.com")).toBe("orders.example.com");
    expect(bareHost("http://octo-int-orders.octo:8080")).toBe(
      "octo-int-orders.octo:8080",
    );
    expect(bareHost("https://host/v1/things")).toBe("host/v1/things");
  });

  // Only a leading scheme goes: an address that carries one later in a path is
  // not two schemes, and rewriting it would corrupt the address being displayed.
  it("only strips a leading scheme", () => {
    expect(bareHost("host/redirect?to=https://elsewhere")).toBe(
      "host/redirect?to=https://elsewhere",
    );
    expect(bareHost("orders.example.com")).toBe("orders.example.com");
  });
});
