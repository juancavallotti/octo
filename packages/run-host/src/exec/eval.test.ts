// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { chmod, mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { evalCel } from "./eval";

/** Writes an executable shell script acting as a stand-in for the octo binary. */
async function fakeBin(dir: string, name: string, body: string): Promise<string> {
  const path = join(dir, name);
  await writeFile(path, `#!/bin/sh\n${body}\n`, "utf8");
  await chmod(path, 0o755);
  return path;
}

describe("evalCel", () => {
  let dir: string;

  beforeEach(async () => {
    dir = await mkdtemp(join(tmpdir(), "octo-eval-"));
  });

  afterEach(() => {
    delete process.env.OCTO_BIN_PATH;
  });

  it("parses the success envelope from stdout", async () => {
    process.env.OCTO_BIN_PATH = await fakeBin(
      dir,
      "octo-eval-ok",
      'printf \'{"ok":true,"result":true}\\n\'',
    );
    const r = await evalCel("body.amount > 100", { data: '{"amount":150}' });
    expect(r.ok).toBe(true);
    expect(r.result).toBe(true);
    expect(r.error).toBeUndefined();
  });

  it("surfaces a compile/eval error envelope (ok:false with error)", async () => {
    process.env.OCTO_BIN_PATH = await fakeBin(
      dir,
      "octo-eval-err",
      'printf \'{"ok":false,"result":null,"error":"no such key: missing"}\\n\'',
    );
    const r = await evalCel("body.missing", { data: "{}" });
    expect(r.ok).toBe(false);
    expect(r.error).toBe("no such key: missing");
  });

  it("forwards expr, data, vars, and env as argv", async () => {
    process.env.OCTO_BIN_PATH = await fakeBin(dir, "octo-eval-args", 'echo "$@"');
    const r = await evalCel("vars.tier", {
      data: '{"x":1}',
      vars: '{"tier":"gold"}',
      env: { REGION: "us" },
    });
    // stdout isn't a valid envelope here, so ok is false — but the echoed argv proves
    // the flags were assembled correctly.
    expect(r.ok).toBe(false);
    expect(r.error).toContain("eval");
    expect(r.error).toContain("-expr vars.tier");
    expect(r.error).toContain('-data {"x":1}');
    expect(r.error).toContain('-vars {"tier":"gold"}');
    expect(r.error).toContain('-env {"REGION":"us"}');
  });

  it("reports a non-zero exit (usage error) as not ok", async () => {
    process.env.OCTO_BIN_PATH = await fakeBin(
      dir,
      "octo-eval-usage",
      '>&2 echo "expression is required (-expr)"\nexit 1',
    );
    const r = await evalCel("");
    expect(r.ok).toBe(false);
    expect(r.error).toContain("expression is required");
  });

  it("flags unparseable stdout", async () => {
    process.env.OCTO_BIN_PATH = await fakeBin(dir, "octo-eval-junk", 'echo not-json');
    const r = await evalCel("body");
    expect(r.ok).toBe(false);
    expect(r.error).toContain("unexpected eval output");
  });

  // Valid JSON of the wrong shape (an object with no `ok`, or a bare scalar) must not
  // slip through as ok:undefined, which would break the boolean the interface promises.
  it("rejects valid JSON that is not a well-formed envelope", async () => {
    process.env.OCTO_BIN_PATH = await fakeBin(
      dir,
      "octo-eval-bad-shape",
      "printf '{\"result\":5}\\n'",
    );
    const r = await evalCel("body");
    expect(r.ok).toBe(false);
    expect(r.error).toContain("unexpected eval output");

    process.env.OCTO_BIN_PATH = await fakeBin(dir, "octo-eval-scalar", "printf '42\\n'");
    const r2 = await evalCel("body");
    expect(r2.ok).toBe(false);
  });

  it("throws when OCTO_BIN_PATH is unset", async () => {
    delete process.env.OCTO_BIN_PATH;
    await expect(evalCel("body")).rejects.toThrow(/OCTO_BIN_PATH/);
  });
});
