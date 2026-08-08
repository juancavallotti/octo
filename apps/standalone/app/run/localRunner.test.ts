// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { mkdtemp, readFile, writeFile, chmod, readdir } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  currentConfigPath,
  localRunner,
  snapshot,
  start,
  status,
  stop,
  sync,
} from "./localRunner";
// The one-shot is here for the single case that proves it leaves a long-running run alone —
// the property that lets it live in @octo/run-host while the runner lives here. It can only
// be checked where both halves exist, which is this app and not the package.
import { invoke } from "@octo/run-host";
import {
  allocateAdminPort,
  allocatePort,
  releaseAdminPort,
  releasePort,
} from "./ports";

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

describe("local runner", () => {
  let dir: string;

  beforeEach(async () => {
    dir = await mkdtemp(join(tmpdir(), "octo-session-"));
    process.env.OCTO_RUN_DIR = dir;
  });

  afterEach(async () => {
    await stop(NS);
    delete process.env.OCTO_BIN_PATH;
    delete process.env.DOLPHIN_BIN_PATH;
    delete process.env.OCTO_RUN_DIR;
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

  // The runtime's observability service binds a fixed :39999 by default, which a
  // second run on this host would fight over. Every run gets an admin port of its
  // own — internal-only ones too, since probes do not need an HTTP source.
  it("gives every run its own admin port, released when it stops", async () => {
    process.env.OCTO_BIN_PATH = await fakeBin(
      dir,
      "octo-admin",
      'printf "admin %s\\n" "$OCTO_OBSERVABILITY_ADDR"\nsleep 2',
    );

    await start(NS, "service:\n  name: internal\n");
    await vi.waitFor(
      () => {
        const line = texts().find((t) => t.startsWith("admin "));
        expect(line).toMatch(/^admin 127\.0\.0\.1:41\d{3}$/);
      },
      { timeout: 4000 },
    );
    const first = texts().find((t) => t.startsWith("admin "))!;

    // A stopped run frees its admin port, so the next run gets the same one back
    // rather than walking up the pool for as long as the editor is up.
    await stop(NS);
    await start(NS, "service:\n  name: internal\n");
    await vi.waitFor(
      () => {
        expect(texts().some((t) => t.startsWith("admin "))).toBe(true);
      },
      { timeout: 4000 },
    );
    expect(texts().find((t) => t.startsWith("admin "))).toBe(first);
  });

  // The dev .env is the user's, but the port wiring is the host's: a stray
  // OCTO_OBSERVABILITY_ADDR must not point two runs at one admin port.
  it("keeps the dev env from clobbering the admin port", async () => {
    process.env.OCTO_BIN_PATH = await fakeBin(
      dir,
      "octo-admin-env",
      'printf "admin %s\\n" "$OCTO_OBSERVABILITY_ADDR"\nsleep 2',
    );

    await start(NS, "service:\n  name: internal\n", {
      OCTO_OBSERVABILITY_ADDR: "127.0.0.1:39999",
    });
    await vi.waitFor(
      () => {
        const line = texts().find((t) => t.startsWith("admin "));
        expect(line).toMatch(/^admin 127\.0\.0\.1:41\d{3}$/);
      },
      { timeout: 4000 },
    );
  });

  // A start that cannot get one of its ports must not keep the other, or leave the
  // config and staged resources behind: a caller retrying a failing start would eat
  // the HTTP pool a port at a time and litter the run dir with env files.
  it("rolls the start back when a port pool is exhausted", async () => {
    process.env.OCTO_BIN_PATH = await fakeBin(dir, "octo-noop", "sleep 1");

    const drained: number[] = [];
    try {
      for (;;) drained.push(allocateAdminPort());
    } catch {
      // Exhausting the admin pool is the point: the next start cannot get one.
    }

    try {
      // Check a port out and back, so we know which one a start would take next.
      const probe = allocatePort();
      releasePort(probe);

      const yaml =
        "service:\n  name: net\nenv:\n  - name: HTTP_PORT\n    default: \"8080\"\n";
      await expect(start(NS, yaml)).rejects.toThrow(/admin port/);

      const after = allocatePort();
      expect(after).toBe(probe); // the failed start kept nothing
      releasePort(after);

      expect(status(NS).running).toBe(false);
      expect(status(NS).port).toBeNull();
      expect(await readdir(join(dir, NS))).toEqual([]); // no config left behind
    } finally {
      for (const p of drained) releaseAdminPort(p);
    }
  });

  // Two Run clicks land as two overlapping start() calls. They must be serialized per
  // namespace: without the lock the second start tears down and re-stages before the
  // first has recorded its child, both octo processes spawn, and the loser is orphaned
  // with its HTTP and admin ports never released (its exit handler sees a different
  // s.proc and frees nothing). Prove the pools are net-zero across two racing starts and
  // one stop — a leak would strand a port for the life of the editor.
  it("serializes overlapping starts so a race orphans no run and leaks no port", async () => {
    process.env.OCTO_BIN_PATH = await fakeBin(dir, "octo-sleep", "echo ready\nsleep 5");
    const yaml =
      'service:\n  name: net\nenv:\n  - name: HTTP_PORT\n    default: "8080"\n';

    // The lowest free port in each pool right now; a net-zero sequence leaves it be.
    const httpBefore = allocatePort();
    releasePort(httpBefore);
    const adminBefore = allocateAdminPort();
    releaseAdminPort(adminBefore);

    const [a, b] = await Promise.all([start(NS, yaml), start(NS, yaml)]);

    // Both calls returned a cleanly running, networked state and exactly one generation
    // is live — the loser was fully torn down, not left half-constructed.
    expect(a.running && b.running).toBe(true);
    expect(status(NS).running).toBe(true);

    await stop(NS);
    expect(status(NS).running).toBe(false);

    // Nothing leaked: the pools hand back the very ports they did before the race.
    const httpAfter = allocatePort();
    releasePort(httpAfter);
    const adminAfter = allocateAdminPort();
    releaseAdminPort(adminAfter);
    expect(httpAfter).toBe(httpBefore);
    expect(adminAfter).toBe(adminBefore);
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

  // The AppRunner surface, which is what a remote backend will also have to satisfy.
  // Only the log stream is genuinely new here — status/start/stop/sync delegate to the
  // functions the tests above already cover.
  describe("log stream", () => {
    const key = { namespace: NS };

    it("replays what is buffered, then follows, and ends when the caller aborts", async () => {
      // "two" is printed a second after "one", so waiting for "one" first makes the
      // second line arrive while the stream is already following — which is the half a
      // buffer replay cannot account for.
      process.env.OCTO_BIN_PATH = await fakeBin(
        dir,
        "octo-drip",
        "echo one\nsleep 1\necho two\nsleep 5",
      );
      await start(NS, "service:\n  name: t\n");
      await vi.waitFor(() => expect(texts()).toContain("one"), { timeout: 4000 });

      const abort = new AbortController();
      const seen: string[] = [];
      // Awaited, so a stream that ignored the abort would fail the test by timing out
      // rather than by leaving a stray promise behind.
      for await (const line of localRunner.logs(key, { signal: abort.signal })) {
        seen.push(line.text);
        if (line.text === "two") abort.abort();
      }

      expect(seen).toContain("one"); // replayed
      expect(seen).toContain("two"); // followed
      expect(seen.some((t) => t.startsWith("▶ starting octo"))).toBe(true);
    });

    // What an SSE reconnect does with its Last-Event-ID: resume, don't replay. Without
    // this the client shows every line twice each time a proxy drops the connection.
    it("resumes after fromSeq instead of replaying", async () => {
      process.env.OCTO_BIN_PATH = await fakeBin(dir, "octo-quick", "echo hello");
      await start(NS, "service:\n  name: t\n");
      await vi.waitFor(
        () => expect(texts().some((t) => t.startsWith("■ runner exited"))).toBe(true),
        { timeout: 4000 },
      );

      const buffered = snapshot(NS);
      expect(buffered.length).toBeGreaterThan(2);
      const last = buffered[buffered.length - 1];

      const abort = new AbortController();
      const seen: number[] = [];
      for await (const line of localRunner.logs(key, {
        fromSeq: buffered[0].seq,
        signal: abort.signal,
      })) {
        seen.push(line.seq);
        if (line.seq === last.seq) abort.abort();
      }

      expect(seen[0]).toBe(buffered[1].seq); // the line at fromSeq is not repeated
      expect(seen).not.toContain(buffered[0].seq);
      expect(seen[seen.length - 1]).toBe(last.seq);
    });

    // A run that is not networked has no address to offer, and a local one never reloads
    // from a save — the editor reads both to decide what to show and whether to push.
    it("reports a local run as pushing, with an app-relative address", async () => {
      process.env.OCTO_BIN_PATH = await fakeBin(dir, "octo-sleep", "sleep 2");
      const yaml =
        'service:\n  name: net\nenv:\n  - name: HTTP_PORT\n    default: "8080"\n';
      const started = await localRunner.start(key, { yaml });
      expect(started.reloadsOnSave).toBe(false);
      expect(started.testUrl).toBe(`/editor/runs/${NS}/`);

      const stopped = await localRunner.stop(key);
      expect(stopped.testUrl).toBeNull();
      expect(await localRunner.status(key)).toEqual(stopped);
    });
  });

  // Two things share one namespace: the long-running run and any one-shot invoked while it
  // is up. They must not collide — the one-shot stages its own config, allocates no port,
  // and writes to no buffer — because the editor's debug controls are used precisely while
  // something is running.
  it("is undisturbed by a one-shot invoke in its own namespace", async () => {
    process.env.OCTO_BIN_PATH = await fakeBin(
      dir,
      "octo-both",
      'if [ "$1" = "invoke" ]; then printf \'{"r":1}\\n\'; else echo ready; sleep 5; fi',
    );

    const yaml =
      'service:\n  name: net\nenv:\n  - name: HTTP_PORT\n    default: "8080"\n';
    const started = await start(NS, yaml);
    await vi.waitFor(() => expect(texts()).toContain("ready"), { timeout: 4000 });
    const port = started.port;

    const r = await invoke(NS, "service:\n  name: t\n", "greet");
    expect(r.output).toContain('{"r":1}');

    expect(texts()).toContain("ready");
    expect(status(NS).port).toBe(port);
    expect(status(NS).running).toBe(true);
  });
});
