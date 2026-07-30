import { describe, it, expect } from "vitest";
import { readFileSync, existsSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

/**
 * The runtime subpath's one invariant: nothing behind it needs a browser.
 *
 * `@octo/editor/runtime` exists so a Node host — an MCP route, a server action — can use
 * the editor's parsers and models without the React editor coming with them. That
 * property is not enforced by anything else, and it is broken by a single convenient
 * re-export: the entrypoint keeps importing fine, and the cost lands on whoever bundles
 * it next, as a stack trace about hooks in a server route.
 *
 * So the guard walks the actual import graph rather than trusting the imports to stay
 * honest. The one thing it tolerates is the icon registry: `resolveIcon` returns icon
 * *components*, and the handful of brand marks that lucide does not ship are written as
 * React components here. Naming one without rendering it is server-safe — which is why
 * the tolerance is spelled as "only the icons", not as "React is fine".
 */

const HERE = dirname(fileURLToPath(import.meta.url));

/** Every source file the runtime subpath pulls in, transitively. */
function graph(entry: string): string[] {
  const seen = new Set<string>();
  const queue = [entry];
  while (queue.length) {
    const file = queue.pop()!;
    if (seen.has(file)) continue;
    seen.add(file);
    for (const spec of specifiers(readFileSync(file, "utf8"))) {
      if (!spec.startsWith(".")) continue;
      const resolved = resolveFile(resolve(dirname(file), spec));
      if (resolved) queue.push(resolved);
    }
  }
  return [...seen];
}

/** Module specifiers, from both `import ... from "x"` and `export ... from "x"`. */
function specifiers(source: string): string[] {
  return [...source.matchAll(/\bfrom\s+"([^"]+)"/g)].map((m) => m[1]);
}

function resolveFile(base: string): string | null {
  for (const candidate of [`${base}.ts`, `${base}.tsx`, `${base}/index.ts`]) {
    if (existsSync(candidate)) return candidate;
  }
  return null;
}

describe("@octo/editor/runtime", () => {
  const files = graph(resolve(HERE, "runtime.ts"));

  it("reaches more than the entrypoint itself", () => {
    // A regex that matched nothing would make every assertion below vacuous.
    expect(files.length).toBeGreaterThan(10);
  });

  it("reaches React only through the icon registry", () => {
    const offenders = files
      .filter((f) => /\bfrom\s+"react(-dom)?"/.test(readFileSync(f, "utf8")))
      .map(short)
      .filter((f) => !/^app\/schema\/[\w-]+-icon\.tsx$/.test(f));

    expect(offenders).toEqual([]);
  });

  it("pulls in no client component", () => {
    const offenders = files.filter((f) =>
      /^\s*"use client"/.test(readFileSync(f, "utf8")),
    );

    expect(offenders.map(short)).toEqual([]);
  });
});

const short = (f: string) => f.slice(HERE.length + 1);
