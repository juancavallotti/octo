// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { chmod, mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { validate } from "./validate";

/** Writes an executable shell script acting as a stand-in for the octo binary. */
async function fakeBin(dir: string, name: string, body: string): Promise<string> {
  const path = join(dir, name);
  await writeFile(path, `#!/bin/sh\n${body}\n`, "utf8");
  await chmod(path, 0o755);
  return path;
}

const files = [
  { path: "orders.yaml", content: "flows:\n  - name: orders\n    process: []\n" },
  { path: "octo.yaml", content: "service:\n  name: shop\n" },
];

describe("validate", () => {
  let dir: string;

  beforeEach(async () => {
    dir = await mkdtemp(join(tmpdir(), "octo-validate-"));
    process.env.OCTO_RUN_DIR = dir;
  });

  afterEach(() => {
    delete process.env.OCTO_BIN_PATH;
    delete process.env.OCTO_RUN_DIR;
  });

  it("stages the project as one folder, declaring the dev env once", async () => {
    // The stand-in reports what it was handed: the folder's files, and which of them
    // declares the dev-env resource.
    process.env.OCTO_BIN_PATH = await fakeBin(
      dir,
      "octo-ls",
      `files=$(ls "$3" | tr '\\n' ' ')
dev=$(grep -l '.env.dev' "$3"/*.yaml | xargs -n1 basename | tr '\\n' ' ')
printf '{"ok":false,"issues":[{"message":"%s"},{"message":"%s"}]}' "$files" "$dev"
exit 1`,
    );
    const r = await validate("ns", files);
    expect(r.ok).toBe(true);
    expect(r.issues.map((i) => i.message.trim())).toEqual(["octo.yaml orders.yaml", "octo.yaml"]);
  });

  it("reads a project with issues as a verdict, not a failure", async () => {
    process.env.OCTO_BIN_PATH = await fakeBin(
      dir,
      "octo-issues",
      `printf '{"ok":false,"issues":[{"where":"orders.ref","message":"flow: flow \\\\"audit\\\\" is not defined"}],"unsetEnv":["API_KEY"]}'
exit 1`,
    );
    const r = await validate("ns", files);
    expect(r).toMatchObject({
      ok: true,
      valid: false,
      issues: [{ where: "orders.ref", message: 'flow: flow "audit" is not defined' }],
      unsetEnv: ["API_KEY"],
    });
  });

  it("is not ok when the runner prints no envelope", async () => {
    process.env.OCTO_BIN_PATH = await fakeBin(dir, "octo-crash", 'echo "boom" >&2\nexit 2');
    const r = await validate("ns", files);
    expect(r.ok).toBe(false);
    expect(r.error).toBe("boom");
  });

  it.each([["flows/orders.yaml"], ["orders_test.yaml"], ["README.md"]])(
    "refuses %s, which the runtime would not load as config",
    async (path) => {
      process.env.OCTO_BIN_PATH = await fakeBin(dir, "octo-unused", "exit 0");
      await expect(validate("ns", [{ path, content: "" }])).rejects.toThrow(path);
    },
  );
});
