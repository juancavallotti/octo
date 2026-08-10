/**
 * Runs outside jsdom: this file reads the Go tree off disk, and under jsdom
 * `import.meta.url` is not a file URL, so it could not find it.
 *
 * @vitest-environment node
 */

import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { BLOCK_CLASSES, classifyBlock, classifySpan, workBreakdown } from "./blockClasses";
import { buildWaterfall } from "./buildWaterfall";
import { records } from "./fixtures";
import type { Waterfall, WaterfallNode } from "./types";

/**
 * The drift test.
 *
 * A classification table in the UI and a block registry in the runtime will come
 * apart, and the failure is silent: a new connector's block lands, nobody
 * classifies it, and every trace that uses it quietly attributes its time to the
 * wrong place. So rather than assert against a list written here — which would
 * pass forever, since it would be the same list twice — this reads the block
 * types out of the Go tree and requires the table to cover them.
 *
 * That makes it a *guard*, not the fix. The fix is to put the classification on
 * the block metadata itself so `octo schema` carries it, at which point this file
 * can go. Until then, a new block fails this test with its own name in the
 * message, which is the shortest path from "a connector landed" to "someone
 * decided whether it waits on the network".
 */

const RUNTIME = fileURLToPath(new URL("../../../../../runtime", import.meta.url));

/** Every non-test Go file under the runtime, by the directory it lives in. */
function goFilesByPackage(): Map<string, string[]> {
  const packages = new Map<string, string[]>();
  const walk = (dir: string) => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const path = join(dir, entry.name);
      if (entry.isDirectory()) {
        walk(path);
      } else if (entry.name.endsWith(".go") && !entry.name.endsWith("_test.go")) {
        const inPackage = packages.get(dir) ?? [];
        inPackage.push(readFileSync(path, "utf8"));
        packages.set(dir, inPackage);
      }
    }
  };
  walk(RUNTIME);
  return packages;
}

/**
 * The string constants one Go package declares, including the `a + "-b"` form the
 * connectors use to build a block name out of their own type.
 */
function constants(sources: string[]): Map<string, string> {
  const values = new Map<string, string>();
  const literal = /(?:^|\b)(\w+)\s*=\s*"([^"]*)"/gm;
  const joined = /(?:^|\b)(\w+)\s*=\s*(\w+)\s*\+\s*"([^"]*)"/gm;

  for (const source of sources) {
    for (const [, name, value] of source.matchAll(literal)) values.set(name, value);
  }
  // Second pass, so a name may be built from a constant declared in any file of
  // the package — which is exactly how the mongodb blocks are spelled.
  for (const source of sources) {
    for (const [, name, base, suffix] of source.matchAll(joined)) {
      const prefix = values.get(base);
      if (prefix !== undefined) values.set(name, prefix + suffix);
    }
  }
  return values;
}

/** Every block type the runtime registers, however it spells it. */
function registeredBlockTypes(): { types: Set<string>; unresolved: string[] } {
  const types = new Set<string>();
  const unresolved: string[] = [];

  // The two places a block announces itself: the factory registry and the schema
  // registry the editor palette is built from.
  // The lookbehind skips the declarations of these two functions, whose own
  // parameter names would otherwise read as a block registered under "name".
  const sites = [
    /(?<!func )MustRegisterBlock\(\s*(?:"([^"]*)"|(\w+))/g,
    /(?<!func )RegisterBlockMeta\(core\.BlockMeta\{[\s\S]{0,300}?Type:\s*(?:"([^"]*)"|([\w.]+))/g,
  ];

  for (const [, sources] of goFilesByPackage()) {
    const values = constants(sources);
    for (const source of sources) {
      for (const pattern of sites) {
        for (const [, quoted, named] of source.matchAll(pattern)) {
          if (quoted !== undefined) {
            types.add(quoted);
            continue;
          }
          const resolved = values.get(named);
          if (resolved !== undefined) types.add(resolved);
          else unresolved.push(named);
        }
      }
    }
  }
  return { types, unresolved };
}

/**
 * Types the runtime injects rather than registers: the debug wrappers, which are
 * deliberately absent from the schema registry because they are never authored.
 * They still reach a trace on a dev run, so the table carries them.
 */
const INJECTED = new Set(["breakpoint", "spy", "mock"]);

describe("the classification table against the runtime's block registry", () => {
  const { types, unresolved } = registeredBlockTypes();

  it("actually found the registry", () => {
    // Without this, a scanner that silently matched nothing would make every
    // assertion below pass by having nothing to check.
    expect(types.size).toBeGreaterThan(30);
    for (const anchor of ["rest", "sql", "fork", "ai-agent", "mongodb-find"]) {
      expect(types, `expected to find ${anchor}`).toContain(anchor);
    }
  });

  it("could read every registration it found", () => {
    // A block registered through a constant this scanner cannot resolve would be
    // invisible to the check below, which is the one way this test could go
    // quiet without anyone noticing.
    expect(unresolved).toEqual([]);
  });

  it("classifies every block the runtime registers", () => {
    const missing = [...types].filter((type) => !(type in BLOCK_CLASSES)).sort();
    expect(
      missing,
      `these block types have no entry in BLOCK_CLASSES — decide whether each one waits on ` +
        `something outside the process ("io"), runs here ("cpu"), or only holds other ` +
        `blocks ("control"): ${missing.join(", ")}`,
    ).toEqual([]);
  });

  it("carries no entry for a block that no longer exists", () => {
    const stale = Object.keys(BLOCK_CLASSES)
      .filter((type) => !types.has(type) && !INJECTED.has(type))
      .sort();
    expect(stale, `these entries name nothing the runtime registers: ${stale.join(", ")}`).toEqual([]);
  });
});

describe("classifySpan", () => {
  const span = (over: Partial<WaterfallNode>): WaterfallNode =>
    ({
      id: "n", kind: "block.post-invoke", label: "x", path: "orders.x", eventId: "ev",
      start: 0, end: 10, durationNs: 10, seq: 0, record: null, input: null,
      inputMatched: true, inferred: false, lane: 0, depth: 0, children: [], ...over,
    }) as WaterfallNode;

  it("reads a model call as I/O whatever block made it", () => {
    // The block is an ai-agent, which is control. The call still went to a
    // provider, and that is where the time went.
    expect(classifySpan(span({ kind: "llm.turn" }))).toBe("io");
    expect(classifySpan(span({ kind: "llm.embed" }))).toBe("io");
  });

  it("reads a span that holds other work as control, whatever its type says", () => {
    // A cache-scope is I/O when it hit and ran nothing. When it missed, its span
    // is its children's, and counting it too would bill those nanoseconds twice.
    const record = { blockType: "cache-scope" } as WaterfallNode["record"];
    expect(classifySpan(span({ record }))).toBe("io");
    expect(classifySpan(span({ record, children: [span({})] }))).toBe("control");
  });

  it("admits when it does not know", () => {
    const record = { blockType: "some-new-connector-block" } as WaterfallNode["record"];
    expect(classifySpan(span({ record }))).toBe("unclassified");
    expect(classifyBlock("")).toBe("unclassified");
  });
});

describe("workBreakdown", () => {
  /** A trace of one flow, with the blocks a test names. */
  function trace(...blocks: { type: string; start: number; duration: number }[]): Waterfall {
    return buildWaterfall(
      records(
        ...blocks.map((b, i) => ({
          kind: "block.post-invoke",
          start: b.start,
          duration: b.duration,
          path: `orders.b${i}`,
          blockType: b.type,
        })),
        { kind: "flow.completed", start: 0, duration: 1_000, flow: "orders" },
      ),
    );
  }

  it("splits a sequential flow between waiting and working", () => {
    const breakdown = workBreakdown(
      trace(
        { type: "rest", start: 0, duration: 600 },
        { type: "multi-transform", start: 600, duration: 200 },
      ),
    );
    expect(breakdown.ioNs).toBe(600);
    expect(breakdown.cpuNs).toBe(200);
    expect(breakdown.unattributedNs).toBe(200);
    expect(breakdown.concurrent).toBe(false);
  });

  /**
   * A fork whose branches ran the given blocks at once. Concurrency has to be
   * modelled through a fork rather than as two blocks of one chain: blocks in a
   * chain run one after another, so a fixture that overlapped them would be
   * asserting against a trace the runtime cannot produce.
   */
  function forked(...branches: { type: string; start: number; duration: number }[]): Waterfall {
    const ends = branches.map((b) => b.start + b.duration);
    return buildWaterfall(
      records(
        ...branches.map((b, i) => ({
          kind: "block.post-invoke",
          start: b.start,
          duration: b.duration,
          path: `orders.fan[${i}].call`,
          blockType: b.type,
        })),
        {
          kind: "block.post-invoke",
          start: 0,
          duration: Math.max(...ends),
          path: "orders.fan",
          blockType: "fork",
        },
        { kind: "flow.completed", start: 0, duration: 1_000, flow: "orders" },
      ),
    );
  }

  it("counts concurrent work once, not once per branch", () => {
    // Four REST calls in a fork, all waiting together. The trace spent 500ns on
    // them; adding the durations up would claim 2000 out of a 1000ns trace.
    const branches = Array.from({ length: 4 }, () => ({
      type: "rest",
      start: 0,
      duration: 500,
    }));
    const breakdown = workBreakdown(forked(...branches));
    expect(breakdown.ioNs).toBe(500);
    expect(breakdown.unattributedNs).toBe(500);
  });

  it("says when the buckets overlap, because a stacked bar would then lie", () => {
    // A REST call and a transform running at once: 400ns of I/O and 400ns of CPU
    // over 400ns of wall clock. Both numbers are true; their sum is not a share.
    const breakdown = workBreakdown(
      forked(
        { type: "rest", start: 0, duration: 400 },
        { type: "multi-transform", start: 0, duration: 400 },
      ),
    );
    expect(breakdown.ioNs).toBe(400);
    expect(breakdown.cpuNs).toBe(400);
    expect(breakdown.coveredNs).toBe(400);
    expect(breakdown.concurrent).toBe(true);
  });

  it("keeps an unknown block visible instead of folding it into CPU", () => {
    const breakdown = workBreakdown(
      trace(
        { type: "rest", start: 0, duration: 300 },
        { type: "brand-new-block", start: 300, duration: 300 },
      ),
    );
    expect(breakdown.cpuNs).toBe(0);
    expect(breakdown.unclassifiedNs).toBe(300);
    expect(breakdown.unknownTypes).toEqual(["brand-new-block"]);
  });

  it("bills a composite's time to what ran inside it", () => {
    // The rest call is inside the fork. Counting both would charge its 600ns
    // twice, and the two buckets would exceed the trace.
    const waterfall = buildWaterfall(
      records(
        { kind: "block.post-invoke", start: 100, duration: 600, path: "orders.fan[0].call", blockType: "rest" },
        { kind: "block.post-invoke", start: 50, duration: 700, path: "orders.fan", blockType: "fork" },
        { kind: "flow.completed", start: 0, duration: 1_000, flow: "orders" },
      ),
    );
    const breakdown = workBreakdown(waterfall);
    expect(breakdown.ioNs).toBe(600);
    expect(breakdown.coveredNs).toBe(600);
    expect(breakdown.unattributedNs).toBe(400);
  });

  it("attributes nothing when nothing ran", () => {
    const breakdown = workBreakdown(buildWaterfall([]));
    expect(breakdown).toMatchObject({
      ioNs: 0,
      cpuNs: 0,
      unclassifiedNs: 0,
      unknownTypes: [],
      concurrent: false,
    });
  });
});
