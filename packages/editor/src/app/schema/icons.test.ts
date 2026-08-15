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
import { Box } from "lucide-react";
import { resolveIcon } from "./index";

/**
 * The drift test.
 *
 * A block's icon is chosen in Go — `core.RegisterBlockMeta{Icon: "Terminal"}` —
 * and resolved here through a hand-maintained map. `resolveIcon` falls back to a
 * generic box for a name it does not know, which is the right runtime behavior
 * and a terrible failure mode at author time: the palette renders, nothing
 * errors, and the block just quietly wears the wrong icon until somebody
 * notices. (Somebody did.)
 *
 * So rather than assert against a list written here — which would be the same
 * list twice — this reads the icon names out of the Go tree and requires every
 * one of them to resolve to something. A new icon fails this test with its own
 * name in the message.
 */

const RUNTIME = fileURLToPath(new URL("../../../../../runtime", import.meta.url));

/** Every `Icon: "Name"` in the runtime's non-test Go sources. */
function iconNamesInGoTree(): Set<string> {
  const names = new Set<string>();
  const walk = (dir: string) => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const path = join(dir, entry.name);
      if (entry.isDirectory()) {
        walk(path);
      } else if (entry.name.endsWith(".go") && !entry.name.endsWith("_test.go")) {
        for (const m of readFileSync(path, "utf8").matchAll(/\bIcon:\s*"([^"]+)"/g)) {
          names.add(m[1]);
        }
      }
    }
  };
  walk(RUNTIME);
  return names;
}

describe("icon registry", () => {
  it("resolves every icon the runtime declares", () => {
    const declared = [...iconNamesInGoTree()].sort();

    // A guard on the guard: if the scrape ever stops finding anything, the test
    // would pass by being vacuous.
    expect(declared.length).toBeGreaterThan(20);

    const unresolved = declared.filter((name) => resolveIcon(name) === Box);
    expect(
      unresolved,
      `these icons are declared in Go but missing from ICONS, so they render as a generic box: ${unresolved.join(", ")}`,
    ).toEqual([]);
  });

  it("still falls back for a name nobody declared", () => {
    // The fallback itself is deliberate — an unknown icon must not throw in the
    // palette. This test exists so the one above cannot be "fixed" by removing it.
    expect(resolveIcon("NotARealIconName")).toBe(Box);
  });
});
