import { describe, expect, it } from "vitest";
import { readdirSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { parseSuite } from "./parse";
import { serializeSuite } from "./serialize";

/**
 * The repo's own dolphin suites, read from `samples/`, are the closest thing to a
 * conformance corpus this model has: they are hand-written, heavily commented, exercise
 * every corner of the format, and `task runtime:test:samples` runs them through the real
 * binaries. If the editor's parser reports an issue on one of them, the parser is wrong
 * — dolphin loads them all.
 *
 * This is deliberately coupled to the repo layout. The alternative is a fixture copied
 * into this package, which would be a snapshot of the format taken on the day it was
 * written and would never notice the format moving.
 */
const SAMPLES = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../../../../../samples",
);

const suites = readdirSync(SAMPLES)
  .filter((f) => /_test\.ya?ml$/i.test(f))
  .sort();

describe("the repo's own suites", () => {
  it("finds them", () => {
    // A path that silently went empty would turn every test below into a no-op.
    expect(suites.length).toBeGreaterThan(20);
  });

  it.each(suites)("%s parses with no issues", (file) => {
    const content = readFileSync(path.join(SAMPLES, file), "utf8");
    const { suite, issues } = parseSuite(content);

    expect(issues).toEqual([]);
    expect(suite.flow).not.toBe("");
    expect(suite.cases.length).toBeGreaterThan(0);
    for (const c of suite.cases) expect(c.name).not.toBe("");
  });

  // Serializing has to produce something the model reads back identically, or the form
  // would quietly lose an assertion the user wrote in YAML.
  it.each(suites)("%s survives a model round trip", (file) => {
    const content = readFileSync(path.join(SAMPLES, file), "utf8");
    const first = parseSuite(content);
    const second = parseSuite(serializeSuite(first.suite));

    expect(second.issues).toEqual([]);
    expect(second.suite).toEqual(first.suite);
  });
});
