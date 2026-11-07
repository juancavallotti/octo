import { describe, expect, it } from "vitest";
import { heldIn, leavesEnvironment, normalizeEnvironment, rolesIn, selectionFromLink } from "./environment";

describe("rolesIn", () => {
  const roles = ["platform:monitor"];
  const envRoles = { staging: ["platform:developer"] };

  it("answers the default by either spelling", () => {
    expect(rolesIn(roles, envRoles, "")).toEqual(roles);
    expect(rolesIn(roles, envRoles, "octo")).toEqual(roles);
  });

  it("answers another environment from its own grants", () => {
    expect(rolesIn(roles, envRoles, "staging")).toEqual(["platform:developer"]);
    expect(rolesIn(roles, envRoles, "qa")).toEqual([]);
  });

  it("carries admin everywhere", () => {
    expect(rolesIn(["platform:admin"], undefined, "qa")).toEqual(["platform:admin"]);
  });
});

describe("normalizeEnvironment", () => {
  it("spells the default as the empty string", () => {
    expect(normalizeEnvironment("octo")).toBe("");
    expect(normalizeEnvironment(undefined)).toBe("");
    expect(normalizeEnvironment(" staging ")).toBe("staging");
  });
});

describe("heldIn", () => {
  it("reads only an environment's own entry, never the prototype's", () => {
    expect(rolesIn([], {}, "constructor")).toEqual([]);
    expect(rolesIn(["platform:admin"], {}, "constructor")).toEqual(["platform:admin"]);
    expect(heldIn({ constructor: ["platform:developer"] }, "constructor")).toEqual(["platform:developer"]);
  });
});

describe("selectionFromLink", () => {
  const staging = { staging: ["platform:developer"] };
  it("chooses the environment a link names, when the person holds a role there", () => {
    expect(selectionFromLink("staging", undefined, [], staging)).toBe("staging");
    expect(selectionFromLink("octo", "staging", ["platform:monitor"], staging)).toBe("");
  });
  it("changes nothing without a parameter, or for the one already chosen", () => {
    expect(selectionFromLink(null, "staging", [], staging)).toBeNull();
    expect(selectionFromLink("staging", "staging", [], staging)).toBeNull();
    expect(selectionFromLink("octo", undefined, [], staging)).toBeNull();
  });
  it("ignores an environment the person holds no role in", () => {
    expect(selectionFromLink("prod", undefined, [], staging)).toBeNull();
  });
});

describe("leavesEnvironment", () => {
  it("is true for an address naming another environment", () => {
    expect(leavesEnvironment("/platform/metrics?env=staging", "")).toBe(true);
    expect(leavesEnvironment("/platform?env=octo", "staging")).toBe(true);
  });
  it("is false for one naming this environment, or none", () => {
    expect(leavesEnvironment("/platform/metrics?env=staging", "staging")).toBe(false);
    expect(leavesEnvironment("/platform?env=octo", "")).toBe(false);
    expect(leavesEnvironment("/platform/metrics", "staging")).toBe(false);
  });
});
