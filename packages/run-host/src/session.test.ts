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

    it("forwards vars and the breakpoint address as argv", async () => {
      process.env.OCTO_BIN_PATH = await fakeBin(dir, "octo-args", 'echo "$@"');
      const r = await invoke(NS, "service:\n  name: t\n", "greet", {
        vars: '{"tier":"gold"}',
        breakAt: "greet.charge",
      });
      expect(r.output).toContain('-vars {"tier":"gold"}');
      expect(r.output).toContain("--break-at greet.charge");
    });

    it("applies logLevel as LOG_LEVEL, but lets an explicit env value win", async () => {
      process.env.OCTO_BIN_PATH = await fakeBin(dir, "octo-level", 'echo "$LOG_LEVEL"');

      const quiet = await invoke(NS, "service:\n  name: t\n", "greet", {
        logLevel: "error",
      });
      expect(quiet.output.trim()).toBe("error");

      // An integration that declares LOG_LEVEL itself must not be silently overridden.
      const explicit = await invoke(NS, "service:\n  name: t\n", "greet", {
        logLevel: "error",
        env: { LOG_LEVEL: "debug" },
      });
      expect(explicit.output.trim()).toBe("debug");
    });

    describe("breakpoints", () => {
      /** A fake runner printing the envelope `octo invoke --break-at` prints. */
      const envelope = (body: string) =>
        `printf '%s\\n' '${body}'`;

      it("decodes a reached envelope into breakpoint", async () => {
        process.env.OCTO_BIN_PATH = await fakeBin(
          dir,
          "octo-break",
          envelope('{"reached":true,"block":"greet.charge","message":{"body":{"amount":250}}}'),
        );

        const r = await invoke(NS, "service:\n  name: t\n", "greet", {
          breakAt: "greet.charge",
        });
        expect(r.ok).toBe(true);
        expect(r.breakpoint?.reached).toBe(true);
        expect(r.breakpoint?.block).toBe("greet.charge");
        expect(r.breakpoint?.message).toEqual({ body: { amount: 250 } });
      });

      // The flow took another branch. A normal debugging result, not a failure.
      it("decodes an unreached envelope without failing the run", async () => {
        process.env.OCTO_BIN_PATH = await fakeBin(
          dir,
          "octo-unreached",
          envelope('{"reached":false,"block":"greet.other"}'),
        );

        const r = await invoke(NS, "service:\n  name: t\n", "greet", {
          breakAt: "greet.other",
        });
        expect(r.ok).toBe(true);
        expect(r.breakpoint?.reached).toBe(false);
        expect(r.breakpoint?.message).toBeUndefined();
      });

      // A flow that failed is reported in-band (exit 0) — the envelope carries why.
      it("surfaces a flow failure carried by the envelope", async () => {
        process.env.OCTO_BIN_PATH = await fakeBin(
          dir,
          "octo-flowerr",
          envelope('{"reached":false,"block":"greet.charge","error":"upstream refused"}'),
        );

        const r = await invoke(NS, "service:\n  name: t\n", "greet", {
          breakAt: "greet.charge",
        });
        expect(r.ok).toBe(true);
        expect(r.breakpoint?.error).toBe("upstream refused");
      });

      // A bad address exits non-zero with nothing on stdout: a bad request, not a
      // "never reached" — it must stay a failure rather than decode to an empty result.
      it("keeps a non-zero exit a failure and reports no breakpoint", async () => {
        process.env.OCTO_BIN_PATH = await fakeBin(
          dir,
          "octo-badaddr",
          '>&2 echo "breakpoint: no flow named \\"nope\\""\nexit 1',
        );

        const r = await invoke(NS, "service:\n  name: t\n", "greet", {
          breakAt: "nope.charge",
        });
        expect(r.ok).toBe(false);
        expect(r.breakpoint).toBeUndefined();
        expect(r.logs.join("\n")).toContain("no flow named");
      });

      it("fails the run when stdout holds no parseable envelope", async () => {
        process.env.OCTO_BIN_PATH = await fakeBin(dir, "octo-garbage", "echo not-json");
        const r = await invoke(NS, "service:\n  name: t\n", "greet", {
          breakAt: "greet.charge",
        });
        expect(r.ok).toBe(false);
        expect(r.breakpoint).toBeUndefined();
        expect(r.logs.join("\n")).toContain("could not parse the debug envelope");
      });

      // Without breakAt, stdout is the flow's result body and must not be sniffed as
      // an envelope — a flow may legitimately return a body with a `reached` field.
      it("does not decode an envelope for a plain invoke", async () => {
        process.env.OCTO_BIN_PATH = await fakeBin(
          dir,
          "octo-plain",
          envelope('{"reached":true,"block":"x"}'),
        );
        const r = await invoke(NS, "service:\n  name: t\n", "greet");
        expect(r.ok).toBe(true);
        expect(r.breakpoint).toBeUndefined();
        expect(r.output).toContain('"reached":true');
      });
    });

    describe("spies and mocks", () => {
      /** A fake runner printing one envelope line, as the CLI does. */
      const envelope = (body: string) => `printf '%s\\n' '${body}'`;

      it("forwards spy addresses as one comma-separated argv value", async () => {
        process.env.OCTO_BIN_PATH = await fakeBin(dir, "octo-args", 'echo "$@"');
        // The fake echoes argv rather than an envelope, so the parse fails and the run is
        // not ok — but stdout is left as it came, which is the argv this pins.
        const r = await invoke(NS, "service:\n  name: t\n", "greet", {
          spies: ["greet.seed", "greet.fanout[audit].log-it"],
        });
        expect(r.output).toContain("--spies greet.seed,greet.fanout[audit].log-it");
      });

      it("forwards mocks as a single JSON blob, and omits the flag when there are none", async () => {
        process.env.OCTO_BIN_PATH = await fakeBin(dir, "octo-args", 'echo "$@"');

        const mocked = await invoke(NS, "service:\n  name: t\n", "greet", {
          mocks: { "greet.charge": { default: { body: { ok: true } } } },
        });
        expect(mocked.output).toContain(
          '--mocks {"greet.charge":{"default":{"body":{"ok":true}}}}',
        );

        // An empty map means "mock nothing", which is what omitting the flag says.
        const empty = await invoke(NS, "service:\n  name: t\n", "greet", { mocks: {} });
        expect(empty.output).not.toContain("--mocks");
      });

      // A spies-only envelope carries no `reached` — that is what keeps it from being
      // read as a breakpoint that never fired.
      it("decodes a spies-only envelope and reports no breakpoint", async () => {
        process.env.OCTO_BIN_PATH = await fakeBin(
          dir,
          "octo-spy",
          envelope(
            '{"result":{"event_id":"e1","body":{"total":9}},"spies":[{"address":"greet.seed","records":[{"seq":1,"at":"2026-07-11T00:00:00Z","input":{"body":{}},"output":{"body":{"total":9}}}]}]}',
          ),
        );

        const r = await invoke(NS, "service:\n  name: t\n", "greet", {
          spies: ["greet.seed"],
        });
        expect(r.ok).toBe(true);
        expect(r.breakpoint).toBeUndefined();
        expect(r.spies?.[0].address).toBe("greet.seed");
        expect(r.spies?.[0].records[0].output).toEqual({ body: { total: 9 } });
      });

      // A spy is read-only: switching one on must not change what a reader of `output`
      // sees, even though stdout now carries an envelope rather than the result message.
      it("re-derives output as the flow's result message under a spies-only run", async () => {
        process.env.OCTO_BIN_PATH = await fakeBin(
          dir,
          "octo-spy-out",
          envelope('{"result":{"event_id":"e1","variables":{},"body":{"total":9}},"spies":[]}'),
        );

        const r = await invoke(NS, "service:\n  name: t\n", "greet", {
          spies: ["greet.seed"],
        });
        expect(JSON.parse(r.output)).toEqual({
          event_id: "e1",
          variables: {},
          body: { total: 9 },
        });
        expect(r.dropped).toBe(false);
      });

      // Under --break-at the flow never produces a result, so there is nothing to report
      // as one — the snapshot in `breakpoint.message` is the thing to read.
      it("reports both the breakpoint and the spies when a run sets both", async () => {
        process.env.OCTO_BIN_PATH = await fakeBin(
          dir,
          "octo-both",
          envelope(
            '{"reached":true,"block":"greet.charge","message":{"body":{"amount":250}},"spies":[{"address":"greet.seed","records":[]}]}',
          ),
        );

        const r = await invoke(NS, "service:\n  name: t\n", "greet", {
          breakAt: "greet.charge",
          spies: ["greet.seed"],
        });
        expect(r.breakpoint?.reached).toBe(true);
        expect(r.breakpoint?.message).toEqual({ body: { amount: 250 } });
        // An empty trace is a result, not a gap: the flow may not have crossed the block.
        expect(r.spies).toEqual([{ address: "greet.seed", records: [] }]);
        expect(r.output).toBe("");
      });

      // Mocking changes what a flow does but observes nothing, so the runner prints a
      // plain result message. Sniffing for an envelope that was never printed would
      // fail a perfectly good run.
      it("does not look for an envelope on a mocks-only run", async () => {
        process.env.OCTO_BIN_PATH = await fakeBin(
          dir,
          "octo-mocked",
          envelope('{"event_id":"e1","variables":{},"body":{"ok":true}}'),
        );

        const r = await invoke(NS, "service:\n  name: t\n", "greet", {
          mocks: { "greet.charge": { default: { body: { ok: true } } } },
        });
        expect(r.ok).toBe(true);
        expect(r.debug).toBeUndefined();
        expect(JSON.parse(r.output)).toEqual({
          event_id: "e1",
          variables: {},
          body: { ok: true },
        });
      });

      // The envelope is the only account of a drop under --spies: the stderr marker the
      // plain path relies on is still printed, but the envelope says so authoritatively.
      it("takes dropped from the envelope on an observing run", async () => {
        process.env.OCTO_BIN_PATH = await fakeBin(
          dir,
          "octo-spy-drop",
          envelope('{"dropped":true,"spies":[]}'),
        );

        const r = await invoke(NS, "service:\n  name: t\n", "greet", {
          spies: ["greet.seed"],
        });
        expect(r.ok).toBe(true);
        expect(r.dropped).toBe(true);
        expect(r.output).toBe("");
      });
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
