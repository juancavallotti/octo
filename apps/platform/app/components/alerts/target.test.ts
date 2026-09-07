import { describe, expect, it } from "vitest";
import {
  applyTarget,
  fillTarget,
  hasTarget,
  scopeFor,
  targetOf,
  targetsAgree,
  NO_TARGET,
} from "./target";
import type { AlertCondition } from "@/app/model/alerts";

const TARGET = {
  integrationId: "i_1",
  deploymentId: "d_1",
  appName: "checkout",
};

function condition(over: Partial<AlertCondition> = {}): AlertCondition {
  return {
    id: "c_1",
    type: "threshold",
    source: "traces",
    metric: "error_rate",
    scope: {},
    ...over,
  };
}

// "One app" is three different predicates underneath, because the three stores
// have three different columns to say it with.
describe("scopeFor", () => {
  it("scopes traces by integration, which survives a rollout", () => {
    expect(scopeFor(TARGET, "traces")).toEqual({ integrationId: "i_1" });
  });

  it("scopes logs by app name, since that table has no integration", () => {
    expect(scopeFor(TARGET, "logs")).toEqual({ appName: "checkout" });
  });

  it("scopes pod stats by deployment, which is all the keys hold", () => {
    expect(scopeFor(TARGET, "pod_stats")).toEqual({ deploymentId: "d_1" });
  });

  // A deployment that never resolved to an integration still has to be
  // watchable; it just cannot survive a rollout, which the picker warns about.
  it("falls back to the deployment when there is no integration", () => {
    const orphan = { ...TARGET, integrationId: "" };
    expect(scopeFor(orphan, "traces")).toEqual({ deploymentId: "d_1" });
  });
});

describe("applyTarget", () => {
  it("rescopes every condition and leaves the refinements alone", () => {
    const before = [
      condition({
        id: "a",
        source: "logs",
        scope: { levels: ["error"], appName: "old" },
      }),
      condition({ id: "b", source: "traces", scope: { integrationId: "old" } }),
    ];
    const after = applyTarget(before, TARGET);

    expect(after[0].scope).toEqual({ levels: ["error"], appName: "checkout" });
    expect(after[1].scope).toEqual({ integrationId: "i_1" });
  });

  // Pod stats are the one source where the watch's deployment may genuinely be
  // the wrong one: an app can have several, and the metrics live under one.
  it("keeps a deployment somebody pinned on a pod-stat condition", () => {
    const before = [
      condition({ source: "pod_stats", scope: { deploymentId: "chosen" } }),
    ];
    expect(applyTarget(before, TARGET)[0].scope).toEqual({
      deploymentId: "chosen",
    });
  });
});

describe("fillTarget", () => {
  // Adding a condition, or changing one's measure, leaves it unscoped — and an
  // unscoped condition is measured over the whole installation.
  it("scopes a condition that has none", () => {
    const filled = fillTarget([condition()], TARGET);
    expect(filled[0].scope).toEqual({ integrationId: "i_1" });
  });

  it("leaves a condition somebody scoped deliberately", () => {
    const pinned = [condition({ scope: { integrationId: "elsewhere" } })];
    expect(fillTarget(pinned, TARGET)[0].scope).toEqual({
      integrationId: "elsewhere",
    });
  });

  it("does nothing at all with no app chosen", () => {
    const before = [condition()];
    expect(fillTarget(before, NO_TARGET)[0].scope).toEqual({});
  });
});

describe("targetOf and targetsAgree", () => {
  it("reads the target back off a stored watch", () => {
    const stored = [
      condition({ source: "traces", scope: { integrationId: "i_1" } }),
      condition({ id: "c_2", source: "logs", scope: { appName: "checkout" } }),
    ];
    expect(targetOf(stored)).toEqual({
      integrationId: "i_1",
      deploymentId: "",
      appName: "checkout",
    });
  });

  // There is no single app that describes conditions pointing at different
  // things, so the editor says so rather than picking one and rewriting the rest.
  it("notices conditions that point at different things", () => {
    const same = [
      condition({ scope: { integrationId: "i_1" } }),
      condition({ id: "c_2", scope: { integrationId: "i_1" } }),
    ];
    const mixed = [
      condition({ scope: { integrationId: "i_1" } }),
      condition({ id: "c_2", scope: { integrationId: "i_2" } }),
    ];
    expect(targetsAgree(same)).toBe(true);
    expect(targetsAgree(mixed)).toBe(false);
  });

  it("knows when nothing has been chosen", () => {
    expect(hasTarget(NO_TARGET)).toBe(false);
    expect(hasTarget(TARGET)).toBe(true);
  });
});

// The editor writes a different scope field per source — integrationId for
// traces, appName for logs, deploymentId for pod stats — so comparing whole
// tuples made a watch this editor had just built report itself as mixed, and
// warn that choosing an app would repoint conditions already pointing there.
describe("targetsAgree across sources", () => {
  it("accepts one app scoped the way each source wants it", () => {
    expect(
      targetsAgree([
        condition({ source: "traces", scope: { integrationId: "i_1" } }),
        condition({ source: "logs", scope: { appName: "checkout" } }),
        condition({ source: "pod_stats", scope: { deploymentId: "d_1" } }),
      ]),
    ).toBe(true);
  });

  it("still refuses two conditions naming the same axis differently", () => {
    expect(
      targetsAgree([
        condition({ source: "traces", scope: { integrationId: "i_1" } }),
        condition({ source: "traces", scope: { integrationId: "i_2" } }),
      ]),
    ).toBe(false);
  });

  it("treats an unscoped condition as agreeing with anything", () => {
    expect(
      targetsAgree([
        condition({ source: "traces", scope: { integrationId: "i_1" } }),
        condition({ source: "logs", scope: {} }),
      ]),
    ).toBe(true);
  });
});
