// @vitest-environment node
import { afterEach, describe, expect, it } from "vitest";
import { allocatePort, isExposable, releasePort } from "./ports";

// The allocator state lives on globalThis; drain it between tests so each starts
// from an empty pool.
afterEach(() => {
  (globalThis as { __octoRunPorts?: Set<number> }).__octoRunPorts = undefined;
});

describe("port allocator", () => {
  it("hands out the lowest free port starting at 40000", () => {
    expect(allocatePort()).toBe(40000);
    expect(allocatePort()).toBe(40001);
    expect(allocatePort()).toBe(40002);
  });

  it("reuses a released port", () => {
    const a = allocatePort(); // 40000
    allocatePort(); // 40001
    releasePort(a);
    expect(allocatePort()).toBe(40000); // lowest free again
  });

  it("release is idempotent", () => {
    const p = allocatePort();
    releasePort(p);
    releasePort(p);
    expect(allocatePort()).toBe(p);
  });
});

describe("isExposable", () => {
  it("is true when HTTP_PORT is declared with a numeric default", () => {
    const yaml = "env:\n  - name: HTTP_PORT\n    default: \"8080\"\n";
    expect(isExposable(yaml)).toBe(true);
  });

  it("accepts an unquoted numeric default", () => {
    const yaml = "env:\n  - name: HTTP_PORT\n    default: 8080\n";
    expect(isExposable(yaml)).toBe(true);
  });

  it("is false without HTTP_PORT", () => {
    const yaml = "env:\n  - name: API_KEY\n    default: x\n";
    expect(isExposable(yaml)).toBe(false);
  });

  it("is false when HTTP_PORT has no usable numeric default", () => {
    expect(isExposable("env:\n  - name: HTTP_PORT\n")).toBe(false);
    expect(isExposable("env:\n  - name: HTTP_PORT\n    default: nope\n")).toBe(false);
    expect(isExposable("env:\n  - name: HTTP_PORT\n    default: \"70000\"\n")).toBe(false);
  });

  it("treats a malformed document as internal-only", () => {
    expect(isExposable(":\n  bad: [")).toBe(false);
  });
});
