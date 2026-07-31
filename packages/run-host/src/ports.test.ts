// @vitest-environment node
import { afterEach, describe, expect, it } from "vitest";
import {
  allocateAdminPort,
  allocatePort,
  isExposable,
  releaseAdminPort,
  releasePort,
} from "./ports";

// The allocator state lives on globalThis; drain it between tests so each starts
// from an empty pool.
afterEach(() => {
  const store = globalThis as {
    __octoRunPorts?: Set<number>;
    __octoRunAdminPorts?: Set<number>;
  };
  store.__octoRunPorts = undefined;
  store.__octoRunAdminPorts = undefined;
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

describe("admin port allocator", () => {
  it("hands out the lowest free port starting at 41000", () => {
    expect(allocateAdminPort()).toBe(41000);
    expect(allocateAdminPort()).toBe(41001);
  });

  it("reuses a released port", () => {
    const a = allocateAdminPort(); // 41000
    allocateAdminPort(); // 41001
    releaseAdminPort(a);
    expect(allocateAdminPort()).toBe(41000);
  });

  // Separate ranges, separate bookkeeping: an admin port must never be handed to a
  // run's HTTP listener, and releasing one must not free the other's.
  it("is independent of the HTTP pool", () => {
    const http = allocatePort();
    const admin = allocateAdminPort();
    expect(admin).toBeGreaterThan(http);

    releasePort(http);
    expect(allocateAdminPort()).toBe(admin + 1); // the admin port stayed taken
    releaseAdminPort(admin);
    expect(allocatePort()).toBe(http); // and freeing it did not disturb the HTTP pool
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
