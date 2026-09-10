import { describe, it, expect } from "vitest";
import { resolveIcon } from "@octo/editor/runtime";
import { iconForDefinition } from "./sourceIcon";

/**
 * The derivation that gives an integration a scannable icon before anyone picks
 * one. Dr. Octo is the case worth pinning: he is an ordinary integration whose
 * transport is http, so without the service-name check he would be drawn as a
 * webhook like any other — and he is the one row on that list nobody should have
 * to read the name of.
 */
const drOcto = `
service:
  name: dr-octo
flows:
  - name: ask
    source:
      connector: http
      type: http-endpoint
      path: /ask
    process:
      - type: ai-agent
        name: dr-octo
        agentId: dr-octo
`;

const plainWebhook = `
service:
  name: notifier
flows:
  - name: hook
    source:
      connector: http
      type: http-endpoint
      path: /hook
    process:
      - type: log
`;

describe("iconForDefinition", () => {
  it("draws the agent's own integration as Dr. Octo", () => {
    expect(iconForDefinition(drOcto)).toBe(resolveIcon("DrOcto"));
  });

  it("leaves every other http integration to the source derivation", () => {
    expect(iconForDefinition(plainWebhook)).not.toBe(resolveIcon("DrOcto"));
  });

  it("survives a definition it cannot parse", () => {
    expect(() => iconForDefinition("{{ not yaml")).not.toThrow();
  });
});
