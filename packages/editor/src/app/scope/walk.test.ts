import { beforeEach, describe, expect, it } from "vitest";
import { newBlock, type BlockNode, type EditorDocument, type FlowDoc } from "../model/document";
import { setCapabilities } from "../schema";
import type { BlockSpec, FieldSpec } from "../schema/types";
import { membersFor, rootsFor } from "./members";
import { buildIndex, scopeAt } from "./walk";

/**
 * The walk is tested through what a user would see in the completion menu — the names
 * offered at a caret — rather than through the Scope structure, because the names are
 * the contract. A rearrangement of the model that keeps them is not a regression.
 */

const spec = (type: string, fields: Partial<FieldSpec>[] = []): BlockSpec => ({
  type,
  label: type,
  group: "Test",
  category: "processor",
  icon: "box",
  description: type,
  fields: fields.map((f) => ({ name: "x", type: "string", label: "x", required: false, ...f }) as FieldSpec),
});

beforeEach(() => {
  setCapabilities({
    blocks: [
      spec("set-variable", [{ name: "name" }, { name: "value", type: "cel" }]),
      spec("delete-variable", [{ name: "name" }]),
      spec("log"),
      spec("rest", [{ name: "statusVar", default: "statusCode" }]),
      spec("tavily-search", [{ name: "resultVar" }]),
      spec("jwt-validate", [{ name: "claimsVar", default: "jwt" }]),
      spec("switch", [{ name: "cases", type: "case-list" }]),
      spec("foreach", [
        { name: "as", default: "item" },
        { name: "body", type: "flow" },
      ]),
      spec("enrich", [
        { name: "setVars", type: "string-map" },
        { name: "body", type: "flow" },
      ]),
    ],
    connectors: [],
  });
});

/** A block with settings, so a test reads as the configuration it describes. */
function block(type: string, settings: Record<string, unknown> = {}): BlockNode {
  return { ...newBlock(type), settings: { ...newBlock(type).settings, ...settings } };
}

function flow(process: BlockNode[], extra: Partial<FlowDoc> = {}): FlowDoc {
  return { id: "flow-1", name: "orders", process, ...extra };
}

function doc(flows: FlowDoc[]): EditorDocument {
  return { flows, connectors: [], processors: [], env: [] };
}

/** The variable names offered at `blockId`. */
function varsAt(document: EditorDocument, blockId: string): string[] {
  const index = buildIndex({ doc: document });
  const members = membersFor(scopeAt(index, { kind: "block", blockId }));
  return (members(["vars"]) ?? []).map((e) => e.name);
}

function rootsAt(document: EditorDocument, site: Parameters<typeof scopeAt>[1]): string[] {
  return rootsFor(scopeAt(buildIndex({ doc: document }), site)).map((e) => e.name);
}

describe("variables from the blocks above", () => {
  it("offers a variable a set-variable upstream declares", () => {
    const set = block("set-variable", { name: "userId" });
    const log = block("log");
    expect(varsAt(doc([flow([set, log])]), log.id)).toContain("userId");
  });

  it("does not offer it to the block that sets it", () => {
    // A block's own settings see the message it RECEIVED, and the variable is not
    // there yet — suggesting it would be confidently wrong.
    const set = block("set-variable", { name: "userId" });
    expect(varsAt(doc([flow([set])]), set.id)).not.toContain("userId");
  });

  it("reads the schema convention, so most blocks need no hand-authoring", () => {
    const search = block("tavily-search", { resultVar: "hits" });
    const log = block("log");
    expect(varsAt(doc([flow([search, log])]), log.id)).toContain("hits");
  });

  it("uses a convention field's schema default when nothing was typed", () => {
    const rest = block("rest");
    const log = block("log");
    expect(varsAt(doc([flow([rest, log])]), log.id)).toContain("statusCode");
    expect(varsAt(doc([flow([block("jwt-validate"), log])]), log.id)).toContain("jwt");
  });

  it("forgets a variable a delete-variable removed", () => {
    const set = block("set-variable", { name: "temp" });
    const del = block("delete-variable", { name: "temp" });
    const log = block("log");
    expect(varsAt(doc([flow([set, del, log])]), log.id)).not.toContain("temp");
  });
});

describe("composites", () => {
  it("carries a switch case's variables out as uncertain, not as absent", () => {
    const inner = block("set-variable", { name: "fromCase" });
    const sw = block("switch");
    sw.slots = { cases: [{ id: "case-1", name: "", process: [inner] }] };
    const after = block("log");

    const document = doc([flow([sw, after])]);
    const index = buildIndex({ doc: document });
    const members = membersFor(scopeAt(index, { kind: "block", blockId: after.id }));
    const entry = (members(["vars"]) ?? []).find((e) => e.name === "fromCase");

    expect(entry).toBeDefined();
    expect(entry?.summary).toMatch(/not on every path/);
  });

  it("scopes foreach's element variable to its body and no further", () => {
    const inner = block("log");
    const each = block("foreach", { as: "row" });
    each.slots = { body: [{ id: "body-1", name: "", process: [inner] }] };
    const after = block("log");

    const document = doc([flow([each, after])]);
    expect(varsAt(document, inner.id)).toContain("row");
    expect(varsAt(document, after.id)).not.toContain("row");
  });

  it("keeps what an enrich body set inside it — the body runs on a copy", () => {
    const inner = block("set-variable", { name: "internal" });
    const en = block("enrich", { setVars: { carried: "body.x" } });
    en.slots = { body: [{ id: "body-1", name: "", process: [inner] }] };
    const after = block("log");

    const document = doc([flow([en, after])]);
    const out = varsAt(document, after.id);
    expect(out).toContain("carried");
    expect(out).not.toContain("internal");
  });
});

describe("roots", () => {
  it("offers the message variables inside a flow", () => {
    const log = block("log");
    const roots = rootsAt(doc([flow([log])]), { kind: "block", blockId: log.id });
    expect(roots).toEqual(expect.arrayContaining(["body", "vars", "env", "now"]));
  });

  it("offers a source payload only what it can compile", () => {
    // Go: SourcePayloadVars is {now, settings} — a source has produced no message,
    // so body and vars are not in scope and must not be suggested.
    const roots = rootsAt(doc([flow([])]), { kind: "source", flowId: "flow-1" });
    expect(roots.sort()).toEqual(["now", "settings"]);
  });

  it("offers the document's declared environment variables", () => {
    const document: EditorDocument = {
      ...doc([flow([])]),
      env: [{ name: "API_KEY" }],
    };
    const index = buildIndex({ doc: document });
    const members = membersFor(scopeAt(index, { kind: "document" }));
    expect((members(["env"]) ?? []).map((e) => e.name)).toContain("API_KEY");
  });
});

describe("test inputs", () => {
  it("seeds body from a saved input, which is the only free evidence there is", () => {
    const log = block("log");
    const document = doc([flow([log])]);
    const index = buildIndex({
      doc: document,
      inputs: new Map([["flow-1", [{ data: '{"orderId":"a","total":3}' }]]]),
    });
    const members = membersFor(scopeAt(index, { kind: "block", blockId: log.id }));
    expect((members(["body"]) ?? []).map((e) => e.name)).toEqual(
      expect.arrayContaining(["orderId", "total"]),
    );
  });

  it("merges several inputs rather than trusting the first", () => {
    const log = block("log");
    const index = buildIndex({
      doc: doc([flow([log])]),
      inputs: new Map([["flow-1", [{ data: '{"a":1}' }, { data: '{"b":2}' }]]]),
    });
    const members = membersFor(scopeAt(index, { kind: "block", blockId: log.id }));
    expect((members(["body"]) ?? []).map((e) => e.name)).toEqual(
      expect.arrayContaining(["a", "b"]),
    );
  });

  it("ignores a half-typed input instead of failing", () => {
    const log = block("log");
    const index = buildIndex({
      doc: doc([flow([log])]),
      inputs: new Map([["flow-1", [{ data: '{"a":' }]]]),
    });
    expect(() => scopeAt(index, { kind: "block", blockId: log.id })).not.toThrow();
  });

  it("drops what it knew about the body once a block replaced it", () => {
    const rest = block("rest");
    const log = block("log");
    const index = buildIndex({
      doc: doc([flow([rest, log])]),
      inputs: new Map([["flow-1", [{ data: '{"orderId":"a"}' }]]]),
    });
    // The REST response is the body now. Still offering `orderId` would be the
    // confidently-wrong suggestion this whole model exists to avoid.
    const members = membersFor(scopeAt(index, { kind: "block", blockId: log.id }));
    expect((members(["body"]) ?? []).map((e) => e.name)).not.toContain("orderId");
  });
});
