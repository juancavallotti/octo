import { describe, expect, it } from "vitest";
import { apiUrl, deployArgs, failure, rolloutArgs, scaleArgs, signInLink, word } from "./args";

/**
 * Every value here comes from a form, and ends up on octopaas's command line: what
 * matters is that none of it can become a flag, and that what the form meant is what
 * the CLI is told.
 */
describe("word", () => {
  it("refuses anything octopaas would read as a flag", () => {
    expect(() => word("--url=evil", "The profile")).toThrow();
    expect(() => word("-p", "The profile")).toThrow();
    expect(word("prod-eu.1", "The profile")).toBe("prod-eu.1");
  });
});

describe("apiUrl", () => {
  it("takes http and https and drops a trailing slash", () => {
    expect(apiUrl(" https://api.example.com/ ")).toBe("https://api.example.com");
    expect(apiUrl("http://localhost:8095")).toBe("http://localhost:8095");
  });

  it("refuses other schemes and non-URLs", () => {
    expect(() => apiUrl("file:///etc/passwd")).toThrow();
    expect(() => apiUrl("api.example.com")).toThrow();
  });
});

describe("signInLink", () => {
  it("is the line that is only a URL", () => {
    expect(signInLink("https://id.example.com/authorize?x=1 ")).toBe("https://id.example.com/authorize?x=1");
    expect(signInLink("! Sign in to prod with id.example.com:")).toBeNull();
    expect(signInLink("")).toBeNull();
  });
});

describe("failure", () => {
  it("keeps what happened and the hint, not the progress before them", () => {
    const stderr = "Deploying billing: pending\nbilling is not running: CrashLoopBackOff\nSee its logs with `octopaas logs billing`.\n";
    expect(failure(stderr, 1)).toBe(
      "billing is not running: CrashLoopBackOff\nSee its logs with `octopaas logs billing`.",
    );
  });

  it("names a cancel as one", () => {
    expect(failure("interrupted", 130)).toBe("Cancelled");
  });

  it("says something when the CLI said nothing", () => {
    expect(failure("", 3)).toBe("octopaas exited with 3");
  });
});

describe("deployArgs", () => {
  it("turns the form into flags, skipping blank rows", () => {
    expect(
      deployArgs("int_1", {
        version: "v1.2.0",
        env: [
          { name: "LOG_LEVEL", value: "info" },
          { name: "", value: "" },
        ],
        secrets: [{ name: "STRIPE_KEY", value: "stripe" }],
        tracing: true,
        replicas: 2,
        subdomain: "billing",
      }),
    ).toEqual([
      "deployments",
      "create",
      "int_1",
      "--version=v1.2.0",
      "--env=LOG_LEVEL=info",
      "--secret=STRIPE_KEY=stripe",
      "--tracing=true",
      "--replicas=2",
      "--expose=external",
      "--subdomain=billing",
    ]);
  });

  it("leaves the version to octopaas when none is chosen", () => {
    expect(deployArgs("int_1", {})).toEqual(["deployments", "create", "int_1"]);
  });

  it("refuses an env name that is not one", () => {
    expect(() => deployArgs("int_1", { env: [{ name: "A B", value: "1" }] })).toThrow();
  });

  it("refuses an id that could be a flag", () => {
    expect(() => deployArgs("--help", {})).toThrow();
  });
});

describe("rolloutArgs", () => {
  it("needs something to roll out", () => {
    expect(() => rolloutArgs("dep_1", {})).toThrow();
    expect(rolloutArgs("dep_1", { version: "v1.0.0" })).toEqual([
      "deployments",
      "rollout",
      "dep_1",
      "--version=v1.0.0",
    ]);
  });
});

describe("scaleArgs", () => {
  it("takes a whole number of replicas", () => {
    expect(scaleArgs("dep_1", 3)).toEqual(["deployments", "scale", "dep_1", "3"]);
    expect(() => scaleArgs("dep_1", 1.5)).toThrow();
    expect(() => scaleArgs("dep_1", -1)).toThrow();
  });
});
