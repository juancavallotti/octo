import { describe, expect, it } from "vitest";
import { heldIn, normalizeEnvironment, rolesIn } from "./environment";

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
