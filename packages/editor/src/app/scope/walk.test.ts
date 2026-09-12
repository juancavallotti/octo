import { beforeEach, describe, expect, it } from "vitest";
import { newBlock, type BlockNode, type EditorDocument, type FlowDoc } from "../model/document";
import { setCapabilities } from "../schema";
import type { BlockSpec, FieldSpec } from "../schema/types";
import { emptyEvidence, fromBlockMocks, fromSuite, fromTestInputs, type Evidence } from "./evidence";
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
      spec("set-payload", [{ name: "value", type: "cel" }, { name: "rawBody", type: "boolean" }]),
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
    connectors: [
      {
        type: "cron",
        label: "Cron",
        category: "trigger",
        icon: "clock",
        description: "",
        fields: [],
        sources: [
          {
            type: "cron",
            label: "Cron schedule",
            icon: "clock",
            description: "",
            fields: [
              { name: "schedule", type: "string", label: "Schedule", required: true },
              { name: "payload", type: "cel", label: "Payload", required: false },
            ],
          },
        ],
      },
    ],
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

/** Evidence carrying saved test inputs for the one flow these tests use. */
function evidenceWithInputs(inputs: Parameters<typeof fromTestInputs>[0]): Evidence {
  const evidence = emptyEvidence();
  evidence.root.set("orders", fromTestInputs(inputs));
  return evidence;
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
      evidence: evidenceWithInputs([{ id: "i", name: "one", data: '{"orderId":"a","total":3}' }]),
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
      evidence: evidenceWithInputs([
        { id: "a", name: "a", data: '{"a":1}' },
        { id: "b", name: "b", data: '{"b":2}' },
      ]),
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
      evidence: evidenceWithInputs([{ id: "i", name: "half", data: '{"a":' }]),
    });
    expect(() => scopeAt(index, { kind: "block", blockId: log.id })).not.toThrow();
  });

  it("drops what it knew about the body once a block replaced it", () => {
    const rest = block("rest");
    const log = block("log");
    const index = buildIndex({
      doc: doc([flow([rest, log])]),
      evidence: evidenceWithInputs([{ id: "i", name: "one", data: '{"orderId":"a"}' }]),
    });
    // The REST response is the body now. Still offering `orderId` would be the
    // confidently-wrong suggestion this whole model exists to avoid.
    const members = membersFor(scopeAt(index, { kind: "block", blockId: log.id }));
    expect((members(["body"]) ?? []).map((e) => e.name)).not.toContain("orderId");
  });
});

describe("evidence about a block", () => {
  it("describes a body that a mock says the block returns", () => {
    // `rest` replaces the body with something the walk alone can only call opaque.
    // A mock on it says exactly what it answers — so downstream, `body.` completes.
    const rest = block("rest");
    rest.name = "charge";
    const after = block("log");
    const document = doc([flow([rest, after])]);

    const evidence = emptyEvidence();
    fromBlockMocks(
      [{ address: "orders.charge", enabled: true, cases: [{ when: "true", body: '{"chargeId":"ch_1"}' }] }],
      evidence,
    );

    const index = buildIndex({ doc: document, evidence });
    const members = membersFor(scopeAt(index, { kind: "block", blockId: after.id }));
    expect((members(["body"]) ?? []).map((e) => e.name)).toContain("chargeId");
  });

  it("does not offer it to the mocked block itself", () => {
    // The mock describes what the block RETURNS. Its own settings see what arrived.
    const rest = block("rest");
    rest.name = "charge";
    const document = doc([flow([rest])]);

    const evidence = emptyEvidence();
    fromBlockMocks(
      [{ address: "orders.charge", enabled: true, cases: [{ when: "true", body: '{"chargeId":"ch_1"}' }] }],
      evidence,
    );

    const index = buildIndex({ doc: document, evidence });
    const members = membersFor(scopeAt(index, { kind: "block", blockId: rest.id }));
    expect((members(["body"]) ?? []).map((e) => e.name)).not.toContain("chargeId");
  });

  it("uses a suite's expectation for what the flow answers with", () => {
    const log = block("log");
    const document = doc([flow([log])]);
    const evidence = emptyEvidence();
    fromSuite(
      { flow: "orders", cases: [{ name: "ok", expect: { body: { receiptUrl: "https://x" } } }] },
      evidence,
    );

    const index = buildIndex({ doc: document, evidence });
    const members = membersFor(scopeAt(index, { kind: "flow-output", flowId: "flow-1" }));
    expect((members(["body"]) ?? []).map((e) => e.name)).toContain("receiptUrl");
  });
});

describe("bodies the flow states outright", () => {
  it("reads the keys a set-payload literal names", () => {
    // The most introspectable thing in a flow, and the case that used to come back
    // "opaque": the expression IS the body, written out.
    const set = block("set-payload", { value: '{"receiptUrl": "https://x", "total": 42}' });
    const after = block("log");
    const members = membersFor(
      scopeAt(buildIndex({ doc: doc([flow([set, after])]) }), { kind: "block", blockId: after.id }),
    );
    expect((members(["body"]) ?? []).map((e) => e.name)).toEqual(
      expect.arrayContaining(["receiptUrl", "total"]),
    );
  });

  it("does not offer it to the block that builds it", () => {
    const set = block("set-payload", { value: '{"receiptUrl": "https://x"}' });
    const members = membersFor(
      scopeAt(buildIndex({ doc: doc([flow([set])]) }), { kind: "block", blockId: set.id }),
    );
    expect((members(["body"]) ?? []).map((e) => e.name)).not.toContain("receiptUrl");
  });

  it("falls back to knowing nothing when the expression is not a literal", () => {
    const set = block("set-payload", { value: "toJson(body)" });
    const after = block("log");
    const index = buildIndex({
      doc: doc([flow([set, after])]),
      evidence: evidenceWithInputs([{ id: "i", name: "one", data: '{"orderId":"a"}' }]),
    });
    const members = membersFor(scopeAt(index, { kind: "block", blockId: after.id }));
    // And it forgets the old body rather than keeping keys that are now wrong.
    expect((members(["body"]) ?? []).map((e) => e.name)).not.toContain("orderId");
  });

  it("types a variable from the literal a set-variable assigns", () => {
    const set = block("set-variable", { name: "receipt", value: '{"url": "https://x"}' });
    const after = block("log");
    const members = membersFor(
      scopeAt(buildIndex({ doc: doc([flow([set, after])]) }), { kind: "block", blockId: after.id }),
    );
    expect((members(["vars", "receipt"]) ?? []).map((e) => e.name)).toContain("url");
  });

  it("follows a variable assigned from a path already in scope", () => {
    // `vars.copy` is whatever `body.user` was — which the test input described.
    const set = block("set-variable", { name: "copy", value: "body.user" });
    const after = block("log");
    const index = buildIndex({
      doc: doc([flow([set, after])]),
      evidence: evidenceWithInputs([
        { id: "i", name: "one", data: '{"user":{"id":"u","email":"e"}}' },
      ]),
    });
    const members = membersFor(scopeAt(index, { kind: "block", blockId: after.id }));
    expect((members(["vars", "copy"]) ?? []).map((e) => e.name)).toEqual(
      expect.arrayContaining(["email", "id"]),
    );
  });
});

describe("the body a source says it will send", () => {
  const cronFlow = (payload?: string): FlowDoc => ({
    ...flow([block("log")]),
    source: {
      connector: "cron",
      type: "cron",
      settings: { schedule: "@every 30s", ...(payload ? { payload } : {}) },
    },
  });

  it("completes from the source's payload expression, with nothing ever run", () => {
    // The whole answer is in the document: the payload is the message this source
    // synthesizes, written out by the user as CEL.
    const f = cronFlow('{"time": string(now), "kind": "tick"}');
    const index = buildIndex({ doc: doc([f]) });
    const members = membersFor(
      scopeAt(index, { kind: "block", blockId: f.process[0].id }),
    );
    expect((members(["body"]) ?? []).map((e) => e.name)).toEqual(
      expect.arrayContaining(["kind", "time"]),
    );
  });

  it("knows nothing when the source declares no payload", () => {
    const f = cronFlow();
    const index = buildIndex({ doc: doc([f]) });
    const members = membersFor(scopeAt(index, { kind: "block", blockId: f.process[0].id }));
    expect(members(["body"])).toBeUndefined();
  });

  it("lets a saved test input win over what the source would send", () => {
    // An input says what this flow was actually called with; the payload only says
    // what would happen if the source fired.
    const f = cronFlow('{"time": string(now)}');
    const evidence = emptyEvidence();
    evidence.root.set("orders", fromTestInputs([{ id: "i", name: "one", data: '{"orderId":"a"}' }]));
    const index = buildIndex({ doc: doc([f]), evidence });
    const members = membersFor(scopeAt(index, { kind: "block", blockId: f.process[0].id }));
    expect((members(["body"]) ?? []).map((e) => e.name)).toContain("orderId");
  });
});
