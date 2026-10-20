import { describe, expect, it } from "vitest";
import { exec } from "./exec";

describe("exec", () => {
  it("hands over a last line that has no newline after it", async () => {
    const lines: string[] = [];
    await exec("/bin/sh", ["-c", "printf 'one\\ntwo'"], { onLine: (l) => lines.push(l) });
    expect(lines).toEqual(["one", "two"]);
  });

  it("says which stream each line came on", async () => {
    const lines: string[] = [];
    await exec("/bin/sh", ["-c", "echo out; echo err >&2"], {
      onLine: (l, stream) => lines.push(`${stream}:${l}`),
    });
    expect(lines.sort()).toEqual(["stderr:err", "stdout:out"]);
  });

  it("writes its input to stdin", async () => {
    const { stdout } = await exec("/bin/sh", ["-c", "cat"], { input: "secret\n" });
    expect(stdout).toBe("secret\n");
  });

  it("keeps only the tail of output it has already streamed", async () => {
    const big = "head -c 300000 /dev/zero | tr '\\0' x; echo; echo last";
    const streamed = await exec("/bin/sh", ["-c", big], { onLine: () => {} });
    expect(streamed.stdout.length).toBeLessThanOrEqual(64 * 1024);
    expect(streamed.stdout.trim().endsWith("last")).toBe(true);
    const whole = await exec("/bin/sh", ["-c", big]);
    expect(whole.stdout.length).toBeGreaterThan(300000);
  });
});
