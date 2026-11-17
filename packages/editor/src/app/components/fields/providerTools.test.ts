import { describe, it, expect, beforeEach } from "vitest";
import type { EditorDocument } from "../../model/document";
import { setCapabilities, type Capabilities } from "../../schema";
import {
  addableTools,
  catalogueFor,
  serializeEntries,
  toEntries,
} from "./providerTools";

const caps: Capabilities = {
  blocks: [],
  connectors: [
    {
      type: "llm-anthropic",
      label: "Anthropic",
      category: "llm",
      settings: [],
      sources: [],
      providerTools: [
        { type: "web_search", label: "Web search", fields: [] },
        { type: "code_execution", label: "Code execution", fields: [] },
      ],
    },
    { type: "llm-plain", label: "Plain", category: "llm", settings: [], sources: [] },
  ],
};

const doc: EditorDocument = {
  connectors: [
    { id: "c1", name: "claude", type: "llm-anthropic", settings: {} },
    { id: "c2", name: "plain", type: "llm-plain", settings: {} },
    { id: "c3", name: "from-env", type: "${LLM_TYPE}", settings: {} },
  ],
  flows: [],
  processors: [],
  env: [],
};

beforeEach(() => setCapabilities(caps));

describe("catalogueFor", () => {
  it("returns the named connection's catalogue", () => {
    expect(catalogueFor(doc, "claude")?.map((t) => t.type)).toEqual([
      "web_search",
      "code_execution",
    ]);
  });

  it("returns an empty catalogue for a connector that runs none", () => {
    expect(catalogueFor(doc, "plain")).toEqual([]);
  });

  it("returns undefined when the connection cannot be resolved", () => {
    expect(catalogueFor(doc, undefined)).toBeUndefined();
    expect(catalogueFor(doc, "missing")).toBeUndefined();
    expect(catalogueFor(doc, "from-env")).toBeUndefined();
  });
});

describe("entries", () => {
  it("reads stored entries and tolerates junk", () => {
    expect(toEntries([{ type: "web_search", settings: { maxUses: 2 } }, null, "x"])).toEqual([
      { type: "web_search", settings: { maxUses: 2 } },
    ]);
    expect(toEntries(undefined)).toEqual([]);
  });

  it("drops cleared settings and empty settings objects", () => {
    expect(
      serializeEntries([
        { type: "web_search", settings: { maxUses: undefined, allowedDomains: [], version: "" } },
        { type: "web_fetch", settings: { citations: true, maxUses: undefined } },
      ]),
    ).toEqual([{ type: "web_search" }, { type: "web_fetch", settings: { citations: true } }]);
  });

  it("offers only the tools not yet enabled", () => {
    const catalogue = catalogueFor(doc, "claude") ?? [];
    expect(addableTools(catalogue, [{ type: "web_search" }]).map((t) => t.type)).toEqual([
      "code_execution",
    ]);
  });
});
