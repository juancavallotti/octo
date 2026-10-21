import path from "node:path";
import { describe, expect, it } from "vitest";
import { installDirs, locate, parseVersion, searchPath } from "./docker";

/**
 * Detection decides whether the container features exist at all, so its two quiet
 * failures matter: a docker the terminal finds that the app does not, and a daemon
 * reported as up when only the CLI answered.
 */

describe("searchPath", () => {
  it("keeps the inherited PATH first and adds only the install dirs it lacks", () => {
    const got = searchPath(["/usr/bin", "/usr/local/bin"].join(path.delimiter), "/home/me").split(
      path.delimiter,
    );
    expect(got.slice(0, 2)).toEqual(["/usr/bin", "/usr/local/bin"]);
    expect(got.filter((d) => d === "/usr/local/bin")).toHaveLength(1);
    expect(got).toContain("/home/me/.orbstack/bin");
  });
});

describe("locate", () => {
  const on = (...files: string[]) => (file: string) => files.includes(file);

  it("finds docker in an install dir a Finder launch's PATH does not have", () => {
    const pathVar = searchPath("/usr/bin", "/home/me");
    const orb = path.join("/home/me/.orbstack/bin", "docker");
    expect(locate(undefined, on(orb), pathVar)).toBe(orb);
  });

  it("prefers the earlier directory", () => {
    const pathVar = ["/a", "/b"].join(path.delimiter);
    expect(locate(undefined, on("/a/docker", "/b/docker"), pathVar)).toBe("/a/docker");
  });

  it("uses the override, and does not fall back when it is missing", () => {
    expect(locate("/opt/docker", on("/opt/docker", "/a/docker"), "/a")).toBe("/opt/docker");
    expect(locate("/opt/docker", on("/a/docker"), "/a")).toBeNull();
  });

  it("is null when there is no docker anywhere", () => {
    expect(locate(undefined, on(), installDirs("/home/me").join(path.delimiter))).toBeNull();
  });
});

describe("parseVersion", () => {
  it("is running when the server answered", () => {
    const out = JSON.stringify({ Client: { Version: "29.7.2" }, Server: { Version: "29.7.1" } });
    expect(parseVersion(out)).toEqual({ state: "running", client: "29.7.2", server: "29.7.1" });
  });

  it("is stopped when only the client answered, with the daemon's complaint", () => {
    const out = JSON.stringify({ Client: { Version: "29.7.2" }, Server: null });
    const err = "Cannot connect to the Docker daemon at unix:///var/run/docker.sock.\n";
    expect(parseVersion(out, err)).toEqual({
      state: "stopped",
      client: "29.7.2",
      error: "Cannot connect to the Docker daemon at unix:///var/run/docker.sock.",
    });
  });

  it("is stopped when the output is not docker's at all", () => {
    expect(parseVersion("garbage").state).toBe("stopped");
  });
});
