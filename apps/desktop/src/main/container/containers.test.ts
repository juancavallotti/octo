import { describe, expect, it, vi } from "vitest";
import { containerName, defaultRef, parseList, runArgs, slug, validRef } from "./containers";

vi.mock("../paths", () => ({ stateDir: () => "" }));

/**
 * The argv is where user input meets a process, so the refusals are the point: a
 * reference that could be read as a flag, or a run that publishes beyond loopback.
 */

describe("validRef", () => {
  it("accepts what a build or push target looks like", () => {
    for (const ref of [
      "octo/orders:dev",
      "localhost:5000/octo/orders:dev",
      "ghcr.io/juan/orders",
      "docker.io/library/orders:0.1.0",
      "us-west1-docker.pkg.dev/proj/octo/orders:v1",
    ]) {
      expect(validRef(ref), ref).toBe(true);
    }
  });

  it("refuses flags, spaces, uppercase repositories and empty parts", () => {
    for (const ref of ["--privileged", "-v", "octo/Orders", "octo orders", "octo//x", "", 7]) {
      expect(validRef(ref), String(ref)).toBe(false);
    }
  });
});

describe("names", () => {
  it("derives image and container names from the folder", () => {
    expect(slug("/Users/me/My Orders!")).toBe("my-orders");
    expect(slug("/tmp/___")).toBe("integration");
    expect(defaultRef("/x/Orders")).toBe("octo/orders:dev");
    expect(containerName("/x/Orders")).toBe("octo-orders");
  });
});

describe("runArgs", () => {
  const plan = {
    name: "octo-orders",
    folder: "/Users/me/orders",
    ref: "octo/orders:dev",
    httpPort: 18080,
    adminPort: 19999,
  };

  it("publishes on loopback only, labelled with the folder", () => {
    const args = runArgs(plan);
    expect(args).toContain("127.0.0.1:18080:8080");
    expect(args).toContain("127.0.0.1:19999:39999");
    expect(args).toContain("dev.octopaas.octo.vault=/Users/me/orders");
    expect(args.at(-1)).toBe("octo/orders:dev");
  });

  it("passes the dev env file, then pins the port the mapping expects", () => {
    const args = runArgs({ ...plan, envFile: "/Users/me/orders/.env.dev" });
    const envFile = args.indexOf("--env-file");
    expect(args[envFile + 1]).toBe("/Users/me/orders/.env.dev");
    expect(args.indexOf("HTTP_PORT=8080")).toBeGreaterThan(envFile);
    expect(runArgs(plan)).not.toContain("--env-file");
  });
});

describe("parseList", () => {
  it("reads docker's tab-separated rows, ports from our labels", () => {
    const out = "abc\tocto-orders\tocto/orders:dev\trunning\tUp 3 minutes\t18080\t19999\n";
    expect(parseList(out)).toEqual([
      {
        id: "abc",
        name: "octo-orders",
        image: "octo/orders:dev",
        state: "running",
        status: "Up 3 minutes",
        httpPort: 18080,
        adminPort: 19999,
      },
    ]);
  });

  it("gives null ports for a container without our labels", () => {
    expect(parseList("abc\tx\ty\texited\tExited (0)\t\t\n")[0]?.httpPort).toBeNull();
  });
});
