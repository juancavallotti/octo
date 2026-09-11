import { describe, expect, it } from "vitest";
import { capabilitiesOf } from "./capabilities";
import {
  PLATFORM_ADMIN,
  PLATFORM_DEVELOPER,
  PLATFORM_MONITOR,
  PLATFORM_OPERATOR,
} from "./roles";

describe("what a caller may do", () => {
  it("gives an administrator all three", () => {
    expect(capabilitiesOf([PLATFORM_ADMIN], true)).toEqual({
      build: true,
      deploy: true,
      administer: true,
    });
  });

  // The split the orchestrator's own policy makes: a developer builds and does
  // not deploy.
  it("lets a developer build but not deploy", () => {
    expect(capabilitiesOf([PLATFORM_DEVELOPER], true)).toEqual({
      build: true,
      deploy: false,
      administer: false,
    });
  });

  it("lets an operator do both", () => {
    expect(capabilitiesOf([PLATFORM_OPERATOR], true)).toEqual({
      build: true,
      deploy: true,
      administer: false,
    });
  });

  it("gives a monitor none of them", () => {
    expect(capabilitiesOf([PLATFORM_MONITOR], true)).toEqual({
      build: false,
      deploy: false,
      administer: false,
    });
  });

  // An installation that has narrowed who writes has the last word over a role,
  // because its gate is the one every write passes through.
  it("takes the two writing verbs away when the installation says so", () => {
    expect(capabilitiesOf([PLATFORM_OPERATOR], false)).toEqual({
      build: false,
      deploy: false,
      administer: false,
    });
  });

  // Except over administering, which is not an operator's setting to make: who
  // changes the installation's own configuration is not AUTH_WRITE_ROLES's
  // business, and withAdmin does not consult it either.
  it("keeps an administrator administering regardless", () => {
    expect(capabilitiesOf([PLATFORM_ADMIN], false).administer).toBe(true);
  });
});
