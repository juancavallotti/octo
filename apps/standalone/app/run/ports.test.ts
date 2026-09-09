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

// A flow with an HTTP source, spelled the way most documents do: the connector's
// address left to the environment.
const httpSource = "flows:\n  - name: api\n    source:\n      connector: api\n      type: http\n";

describe("isExposable", () => {
  // The address is this host's to supply, so these are the shapes that take it.
  describe("a listener that takes the injected address", () => {
    it("is true for an HTTP source with no env block and no connector", () => {
      expect(isExposable("flows:\n  - name: api\n    source:\n      type: http\n")).toBe(true);
    });

    it("is true when the source names the connector type in the binding", () => {
      const yaml = "flows:\n  - name: api\n    source:\n      connector: http\n      type: http\n";
      expect(isExposable(yaml)).toBe(true);
    });

    it("is true for a configured connector with no settings", () => {
      expect(isExposable("connectors:\n  - name: api\n    type: http\n" + httpSource)).toBe(true);
    });

    it("is true when a lone configured connector binds an unnamed source", () => {
      const yaml =
        "connectors:\n  - name: api\n    type: http\n" +
        "flows:\n  - name: f\n    source:\n      type: http\n";
      expect(isExposable(yaml)).toBe(true);
    });

    it("is true when the connector substitutes the declared HTTP_PORT", () => {
      const yaml =
        'env:\n  - name: HTTP_PORT\n    default: "8080"\n' +
        "connectors:\n  - name: api\n    type: http\n    settings:\n      port: ${HTTP_PORT}\n" +
        httpSource;
      expect(isExposable(yaml)).toBe(true);
    });

    it("is true when host and port are both substituted", () => {
      const yaml =
        'env:\n  - name: HTTP_HOST\n    default: "0.0.0.0"\n  - name: HTTP_PORT\n    default: "8080"\n' +
        "connectors:\n  - name: api\n    type: http\n    settings:\n" +
        "      host: ${HTTP_HOST}\n      port: ${HTTP_PORT}\n" +
        httpSource;
      expect(isExposable(yaml)).toBe(true);
    });

    it("is true when the connector pins bind-all as its host", () => {
      const yaml =
        "connectors:\n  - name: api\n    type: http\n    settings:\n      host: 0.0.0.0\n" + httpSource;
      expect(isExposable(yaml)).toBe(true);
    });

    it("is true when settings other than the address are pinned", () => {
      const yaml =
        "connectors:\n  - name: api\n    type: http\n    settings:\n" +
        "      basePath: /api/v1\n      requestTimeout: 5s\n" +
        httpSource;
      expect(isExposable(yaml)).toBe(true);
    });
  });

  // Each of these is a run this host would proxy into a void.
  describe("a listener the injected address does not reach", () => {
    // Settings beat the environment in the runtime, so the child serves its own
    // port and the proxy points at nothing.
    it("is false when the connector pins its own port", () => {
      const yaml =
        "connectors:\n  - name: api\n    type: http\n    settings:\n      port: 9000\n" + httpSource;
      expect(isExposable(yaml)).toBe(false);
    });

    it("is false when the pinned port is a string", () => {
      const yaml =
        'connectors:\n  - name: api\n    type: http\n    settings:\n      port: "9000"\n' + httpSource;
      expect(isExposable(yaml)).toBe(false);
    });

    // The declaration is not what makes a listener: nothing reads it here.
    it("is false when a declared HTTP_PORT is pinned over", () => {
      const yaml =
        'env:\n  - name: HTTP_PORT\n    default: "8080"\n' +
        "connectors:\n  - name: api\n    type: http\n    settings:\n      port: 9000\n" +
        httpSource;
      expect(isExposable(yaml)).toBe(false);
    });

    it("is false when the port is OS-assigned", () => {
      const yaml =
        "connectors:\n  - name: api\n    type: http\n    settings:\n      port: 0\n" + httpSource;
      expect(isExposable(yaml)).toBe(false);
    });

    it("is false when the port comes from another variable", () => {
      const yaml =
        'env:\n  - name: API_PORT\n    default: "9000"\n' +
        "connectors:\n  - name: api\n    type: http\n    settings:\n      port: ${API_PORT}\n" +
        httpSource;
      expect(isExposable(yaml)).toBe(false);
    });

    // Referencing an undeclared variable is a load error, not a listener.
    it("is false when ${HTTP_PORT} is never declared", () => {
      const yaml =
        "connectors:\n  - name: api\n    type: http\n    settings:\n      port: ${HTTP_PORT}\n" +
        httpSource;
      expect(isExposable(yaml)).toBe(false);
    });

    it("is false when the connector pins a host of its own", () => {
      const yaml =
        "connectors:\n  - name: api\n    type: http\n    settings:\n      host: 192.168.1.5\n" + httpSource;
      expect(isExposable(yaml)).toBe(false);
    });

    // Both would take the injected port and the second one's bind fails, taking
    // the run down with it.
    it("is false when two connectors are both left to the environment", () => {
      const yaml = "connectors:\n  - name: api\n    type: http\n  - name: admin\n    type: http\n" + httpSource;
      expect(isExposable(yaml)).toBe(false);
    });

    it("is true again when the second connector owns its port", () => {
      const yaml =
        "connectors:\n  - name: api\n    type: http\n" +
        "  - name: admin\n    type: http\n    settings:\n      port: 9000\n" +
        httpSource;
      expect(isExposable(yaml)).toBe(true);
    });

    it("is false when the binding is ambiguous", () => {
      const yaml =
        "connectors:\n  - name: a\n    type: http\n" +
        "  - name: b\n    type: http\n    settings:\n      port: 9000\n" +
        "flows:\n  - name: f\n    source:\n      type: http\n";
      expect(isExposable(yaml)).toBe(false);
    });

    // A listener with no routes answers 404; there is no endpoint to offer.
    it("is false for a declared HTTP_PORT with no HTTP source", () => {
      expect(isExposable('env:\n  - name: HTTP_PORT\n    default: "8080"\n')).toBe(false);
      const withConnector =
        'env:\n  - name: HTTP_PORT\n    default: "8080"\n' +
        "connectors:\n  - name: api\n    type: http\n" +
        "flows:\n  - name: f\n    source:\n      type: cron\n";
      expect(isExposable(withConnector)).toBe(false);
    });

    it("is false for non-HTTP and sourceless flows", () => {
      expect(isExposable("flows:\n  - name: f\n    source:\n      type: cron\n")).toBe(false);
      expect(isExposable("flows:\n  - name: f\n    process:\n      - type: log\n")).toBe(false);
    });

    // A binding that names neither a configured instance nor the type does not
    // resolve in the runtime either; it fails to start rather than listening.
    it("is false for an unresolvable binding", () => {
      const yaml = "flows:\n  - name: f\n    source:\n      connector: nope\n      type: http\n";
      expect(isExposable(yaml)).toBe(false);
    });

    it("treats a malformed document as internal-only", () => {
      expect(isExposable(":\n  bad: [")).toBe(false);
    });
  });
});
