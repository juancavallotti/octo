// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { mkdtemp, readFile, writeFile, chmod, readdir } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  currentConfigPath,
  evalCel,
  invoke,
  snapshot,
  start,
  status,
  stop,
  sync,
} from "./session";

/** Fixed namespace for the single-user test surface. */
const NS = "testns00";

/** Writes an executable shell script acting as a stand-in for the octo binary. */
async function fakeBin(dir: string, name: string, body: string): Promise<string> {
  const path = join(dir, name);
  await writeFile(path, `#!/bin/sh\n${body}\n`, "utf8");
  await chmod(path, 0o755);
  return path;
}

const texts = () => snapshot(NS).map((l) => l.text);

describe("run session", () => {
  let dir: string;

  beforeEach(async () => {
    dir = await mkdtemp(join(tmpdir(), "octo-session-"));
    process.env.OCTO_RUN_DIR = dir;
  });

  afterEach(async () => {
    await stop(NS);
    delete process.env.OCTO_BIN_PATH;
    delete process.env.OCTO_RUN_DIR;
  });

  it("reports availability from OCTO_BIN_PATH", () => {
    delete process.env.OCTO_BIN_PATH;
    expect(status(NS).available).toBe(false);
    process.env.OCTO_BIN_PATH = "/somewhere/octo";
    expect(status(NS).available).toBe(true);
  });

  it("captures runner output line-by-line and tracks exit", async () => {
    process.env.OCTO_BIN_PATH = await fakeBin(
      dir,
      "octo-print",
      'printf "line one\\nline two\\n"',
    );

    const started = await start(NS, "service:\n  name: t\n");
    expect(started.running).toBe(true);

    await vi.waitFor(
      () => {
        const out = texts();
        expect(out).toContain("line one");
        expect(out).toContain("line two");
        expect(out.some((t) => t.startsWith("■ runner exited"))).toBe(true);
      },
      { timeout: 4000 },
    );

    expect(status(NS).running).toBe(false);
    expect(texts().some((t) => t.startsWith("▶ starting octo"))).toBe(true);
  });

  it("hot-reloads by rewriting the same config file on sync", async () => {
    process.env.OCTO_BIN_PATH = await fakeBin(
      dir,
      "octo-sleep",
      'echo ready\nsleep 2',
    );

    await start(NS, "service:\n  name: first\n");
    await vi.waitFor(() => expect(texts()).toContain("ready"), { timeout: 4000 });

    const path = currentConfigPath(NS);
    expect(path).toBeTruthy();
    expect(await readFile(path!, "utf8")).toContain("first");

    await sync(NS, "service:\n  name: second\n");
    expect(await readFile(path!, "utf8")).toContain("second");

    const stopped = await stop(NS);
    expect(stopped.running).toBe(false);
    // The rendered config (in the namespace's directory) is cleaned up on stop.
    expect(await readdir(join(dir, NS))).not.toContain(path!.split("/").pop());
  });

  it("allocates a port and injects HTTP_PORT for a networked run", async () => {
    process.env.OCTO_BIN_PATH = await fakeBin(
      dir,
      "octo-port",
      'printf "bound %s on %s\\n" "$HTTP_PORT" "$HTTP_HOST"',
    );

    const yaml =
      "service:\n  name: net\nenv:\n  - name: HTTP_PORT\n    default: \"8080\"\n";
    const started = await start(NS, yaml);
    expect(started.exposable).toBe(true);
    expect(started.port).toBeGreaterThanOrEqual(40000);

    await vi.waitFor(
      () => {
        expect(texts()).toContain(`bound ${started.port} on 127.0.0.1`);
      },
      { timeout: 4000 },
    );

    // The port is released once the run is stopped.
    await stop(NS);
    expect(status(NS).port).toBeNull();
    expect(status(NS).exposable).toBe(false);
  });

  it("does not allocate a port for an internal-only run", async () => {
    process.env.OCTO_BIN_PATH = await fakeBin(dir, "octo-noop", "sleep 1");
    const started = await start(NS, "service:\n  name: internal\n");
    expect(started.exposable).toBe(false);
    expect(started.port).toBeNull();
  });

  it("ignores sync when nothing is running", async () => {
    delete process.env.OCTO_BIN_PATH;
    const result = await sync(NS, "service:\n  name: x\n");
    expect(result.running).toBe(false);
  });

  describe("resource staging", () => {
    const yamlWith = (env: string[]) =>
      `resources:\n  env:\n${env.map((e) => `    - ${e}`).join("\n")}\n`;

    it("stages declared resources into the run dir before the config, and removes them on stop", async () => {
      process.env.OCTO_BIN_PATH = await fakeBin(dir, "octo-sleep", "echo ready\nsleep 2");
      const calls: string[][] = [];
      await start(NS, yamlWith([".env.dev"]), undefined, {
        resources: async (names) => {
          calls.push(names);
          return [{ name: ".env.dev", content: "A=1\n" }];
        },
      });
      expect(calls).toEqual([[".env.dev"]]);
      expect(await readFile(join(dir, NS, ".env.dev"), "utf8")).toBe("A=1\n");

      await stop(NS);
      expect(await readdir(join(dir, NS))).not.toContain(".env.dev");
    });

    it("does not call the provider on a content-only sync (unchanged resource set)", async () => {
      process.env.OCTO_BIN_PATH = await fakeBin(dir, "octo-sleep", "echo ready\nsleep 3");
      let calls = 0;
      const provider = async () => {
        calls += 1;
        return [{ name: ".env.dev", content: "A=1" }];
      };
      await start(NS, yamlWith([".env.dev"]), undefined, { resources: provider });
      await vi.waitFor(() => expect(texts()).toContain("ready"), { timeout: 4000 });
      expect(calls).toBe(1);

      await sync(NS, yamlWith([".env.dev"]) + "service:\n  name: edited\n", {
        resources: provider,
      });
      expect(calls).toBe(1); // set unchanged → provider not re-invoked
    });

    it("re-stages and prunes de-declared files when the resource set changes on sync", async () => {
      process.env.OCTO_BIN_PATH = await fakeBin(dir, "octo-sleep", "echo ready\nsleep 3");
      const provider = async (names: string[]) =>
        names.map((name) => ({ name, content: `${name}=1` }));
      // Declare an extra env resource alongside the always-present .env.dev.
      await start(NS, yamlWith([".env.dev", ".env.extra"]), undefined, {
        resources: provider,
      });
      await vi.waitFor(() => expect(texts()).toContain("ready"), { timeout: 4000 });
      expect(await readdir(join(dir, NS))).toContain(".env.extra");

      // Drop .env.extra; .env.dev stays (it is always effectively declared).
      await sync(NS, yamlWith([".env.dev"]), { resources: provider });
      const left = await readdir(join(dir, NS));
      expect(left).toContain(".env.dev");
      expect(left).not.toContain(".env.extra"); // no-longer-declared file pruned
    });
  });

  describe("invoke", () => {
    it("returns stdout as the result and stderr as separate log lines", async () => {
      process.env.OCTO_BIN_PATH = await fakeBin(
        dir,
        "octo-invoke",
        'printf \'{"ok":true}\\n\'\n>&2 printf "log one\\nlog two\\n"',
      );

      const r = await invoke(NS, "service:\n  name: t\n", "greet");
      expect(r.ok).toBe(true);
      expect(r.exitCode).toBe(0);
      expect(r.timedOut).toBe(false);
      expect(r.output).toContain('{"ok":true}');
      expect(r.logs).toEqual(["log one", "log two"]);
    });

    it("reports a non-zero exit as not ok", async () => {
      process.env.OCTO_BIN_PATH = await fakeBin(dir, "octo-fail", "exit 1");
      const r = await invoke(NS, "service:\n  name: t\n", "greet");
      expect(r.ok).toBe(false);
      expect(r.exitCode).toBe(1);
      expect(r.dropped).toBe(false);
    });

    it("flags a dropped message from the runner's stderr marker", async () => {
      process.env.OCTO_BIN_PATH = await fakeBin(
        dir,
        "octo-drop",
        '>&2 echo \'time=... level=INFO msg="flow dropped the message" flow=greet\'',
      );
      const r = await invoke(NS, "service:\n  name: t\n", "greet");
      expect(r.ok).toBe(true);
      expect(r.dropped).toBe(true);
      expect(r.output).toBe("");
    });

    it("forwards the flow, data, and timeout as argv", async () => {
      process.env.OCTO_BIN_PATH = await fakeBin(dir, "octo-args", 'echo "$@"');
      const r = await invoke(NS, "service:\n  name: t\n", "greet", {
        data: '{"x":1}',
        timeoutMs: 5000,
      });
      expect(r.output).toContain("invoke");
      expect(r.output).toContain("-flow greet");
      expect(r.output).toContain("-data {\"x\":1}");
      expect(r.output).toContain("-timeout 5000ms");
    });

    it("injects env vars into the runner", async () => {
      process.env.OCTO_BIN_PATH = await fakeBin(
        dir,
        "octo-env",
        'echo "$API_KEY"',
      );
      const r = await invoke(NS, "service:\n  name: t\n", "greet", {
        env: { API_KEY: "sekret" },
      });
      expect(r.output).toContain("sekret");
    });

    it("force-kills a runner that exceeds the wall-clock budget and cleans up", async () => {
      // The backstop fires at timeoutMs + INVOKE_GRACE_MS (~5.1s here, since the fake
      // ignores the CLI -timeout), so allow more than vitest's default 5s per-test cap.
      process.env.OCTO_BIN_PATH = await fakeBin(dir, "octo-hang", "sleep 30");
      const r = await invoke(NS, "service:\n  name: t\n", "greet", {
        timeoutMs: 100,
      });
      expect(r.timedOut).toBe(true);
      expect(r.ok).toBe(false);
      // The throwaway invoke dir is removed even when the run had to be killed.
      const left = await readdir(join(dir, NS));
      expect(left.some((f) => f.startsWith("invoke-"))).toBe(false);
    }, 10000);

    it("removes the throwaway invoke dir after a successful run", async () => {
      process.env.OCTO_BIN_PATH = await fakeBin(dir, "octo-ok", "echo done");
      await invoke(NS, "service:\n  name: t\n", "greet");
      const left = await readdir(join(dir, NS));
      expect(left.some((f) => f.startsWith("invoke-"))).toBe(false);
    });

    it("stages declared resources beside the invoke config and removes them after", async () => {
      // The fake octo lists its config's own directory so we can see what was staged.
      process.env.OCTO_BIN_PATH = await fakeBin(
        dir,
        "octo-lsdir",
        'shift; ls -A "$(dirname "$2")"',
      );
      const yaml = "resources:\n  env:\n    - .env.dev\n";
      const r = await invoke(NS, yaml, "greet", {
        resources: async () => [{ name: ".env.dev", content: "A=1" }],
      });
      expect(r.output).toContain(".env.dev");
      // Nothing is left behind in the namespace dir once invoke returns.
      const left = await readdir(join(dir, NS)).catch(() => []);
      expect(left.some((f) => f.startsWith("invoke-"))).toBe(false);
    });

    it("does not disturb a concurrent long-running run in the same namespace", async () => {
      process.env.OCTO_BIN_PATH = await fakeBin(
        dir,
        "octo-both",
        'if [ "$1" = "invoke" ]; then printf \'{"r":1}\\n\'; else echo ready; sleep 5; fi',
      );

      const yaml =
        "service:\n  name: net\nenv:\n  - name: HTTP_PORT\n    default: \"8080\"\n";
      const started = await start(NS, yaml);
      await vi.waitFor(() => expect(texts()).toContain("ready"), { timeout: 4000 });
      const port = started.port;

      const r = await invoke(NS, "service:\n  name: t\n", "greet");
      expect(r.output).toContain('{"r":1}');

      // The long-running run's log buffer and allocated port are untouched by invoke.
      expect(texts()).toContain("ready");
      expect(status(NS).port).toBe(port);
      expect(status(NS).running).toBe(true);
    });

    it("throws when OCTO_BIN_PATH is unset", async () => {
      delete process.env.OCTO_BIN_PATH;
      await expect(invoke(NS, "service:\n  name: t\n", "greet")).rejects.toThrow(
        /OCTO_BIN_PATH/,
      );
    });
  });

  describe("evalCel", () => {
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

    it("throws when OCTO_BIN_PATH is unset", async () => {
      delete process.env.OCTO_BIN_PATH;
      await expect(evalCel("body")).rejects.toThrow(/OCTO_BIN_PATH/);
    });
  });
});
