import { describe, expect, it } from "vitest";
import { newBlock } from "../model/document";
import { setCapabilities } from "../schema";
import type { Capabilities } from "../schema/types";
import { contributionOf } from "./contributions";

/**
 * The conventional tier, which is what keeps the hand-written table small.
 *
 * The *rot alarm* that guards it is not here: it has to read the real catalogue, and
 * the bundled capabilities.json is an empty fallback — the editor loads the schema
 * from the binary at boot. A test against that file would pass vacuously forever, so
 * the check runs `bin/octo schema` from scripts/check-scope-contributions.mjs in CI,
 * beside the docs-drift check that has the same problem and the same answer.
 */

describe("contributionOf", () => {
  const caps: Capabilities = {
    blocks: [
      {
        type: "thing",
        label: "Thing",
        group: "Test",
        category: "processor",
        icon: "box",
        description: "",
        fields: [
          { name: "resultVar", type: "string", label: "Result", required: false },
          { name: "statusVar", type: "string", label: "Status", required: false, default: "code" },
          { name: "plain", type: "string", label: "Plain", required: false },
        ],
      },
    ],
    connectors: [],
  };

  it("reads a conventionally named setting as a variable declaration", () => {
    setCapabilities(caps);
    const block = { ...newBlock("thing"), settings: { resultVar: "hits" } };
    expect(Object.keys(contributionOf(block).setVars ?? {})).toContain("hits");
  });

  it("falls back to the schema default, which is what the block will use", () => {
    setCapabilities(caps);
    expect(Object.keys(contributionOf(newBlock("thing")).setVars ?? {})).toContain("code");
  });

  it("types the variables the runtime documents", () => {
    setCapabilities(caps);
    const status = contributionOf(newBlock("thing")).setVars?.code;
    expect(status?.shape).toEqual({ kind: "number" });
  });

  it("ignores a setting that merely holds a string", () => {
    setCapabilities(caps);
    const block = { ...newBlock("thing"), settings: { plain: "hello" } };
    expect(Object.keys(contributionOf(block).setVars ?? {})).not.toContain("hello");
  });

  it("treats an unknown block type as having done anything at all", () => {
    setCapabilities({ blocks: [], connectors: [] });
    // Silence, not guesses: nothing is declared and the body is no longer described.
    expect(contributionOf(newBlock("who-knows")).body).toBe("opaque");
  });
});
