import { describe, expect, it } from "vitest";
import { hostSet, isIntegrationHost } from "./hostGuard";

const BASE = "apps.example.com";

describe("isIntegrationHost", () => {
  const guard = (host: string, platformHosts = "") =>
    isIntegrationHost(host, BASE, hostSet(platformHosts));

  it("catches a hostname under the wildcard", () => {
    expect(guard("notathing.apps.example.com")).toBe(true);
    expect(guard("my-integration.apps.example.com")).toBe(true);
  });

  it("ignores the port and the case of the Host header", () => {
    expect(guard("NotAThing.Apps.Example.Com:8443")).toBe(true);
  });

  it("leaves hostnames outside the wildcard alone", () => {
    expect(guard("octo.example.com")).toBe(false);
    expect(guard("example.com")).toBe(false);
    // A suffix match on the bare string would wrongly catch this one.
    expect(guard("evilapps.example.com")).toBe(false);
  });

  it("leaves the apex of the wildcard alone", () => {
    // *.apps.example.com does not include apps.example.com itself, and something
    // unrelated may well be served there.
    expect(guard("apps.example.com")).toBe(false);
  });

  /**
   * The lockout case, and the reason this function is tested at all. The editor
   * may legitimately be published under the wildcard; treating its own hostname as
   * an integration host would serve it the error page and make the platform
   * unreachable for everyone.
   */
  it("never treats the editor's own hostname as an integration host", () => {
    expect(guard("octo.apps.example.com", "octo.apps.example.com")).toBe(false);
    // Still true for anything else under the same wildcard.
    expect(guard("something.apps.example.com", "octo.apps.example.com")).toBe(
      true,
    );
  });

  it("honours every editor hostname the chart passes, not just the first", () => {
    const hosts = "octo.apps.example.com,alias.apps.example.com";
    expect(guard("alias.apps.example.com", hosts)).toBe(false);
  });

  it("is off entirely when no wildcard domain is configured", () => {
    // Local development: there is no integrations domain to be confused with.
    expect(isIntegrationHost("anything.at.all", "", new Set())).toBe(false);
    expect(isIntegrationHost("anything.at.all", undefined, new Set())).toBe(
      false,
    );
  });

  it("says no when there is no Host header to judge", () => {
    expect(guard("")).toBe(false);
  });
});

describe("hostSet", () => {
  it("normalizes and drops the empties", () => {
    expect([...hostSet(" A.Example.com:443 , ,b.example.com ")]).toEqual([
      "a.example.com",
      "b.example.com",
    ]);
  });

  it("is empty when the chart passed nothing", () => {
    expect(hostSet(undefined).size).toBe(0);
  });
});
