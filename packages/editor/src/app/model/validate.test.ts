import { describe, it, expect } from "vitest";
import {
  EditorDocument,
  blankDocument,
  emptyFlow,
  newBlock,
} from "./document";
import { issueMessages, validateDocument } from "./validate";

/** A minimal runnable document: a cron-driven flow that logs. */
function runnableDoc(): EditorDocument {
  const flow = emptyFlow("ticker");
  flow.source = {
    connector: "cron",
    connectorRef: "clock",
    type: "cron",
    settings: { schedule: "@every 2s" },
  };
  flow.process = [newBlock("log")]; // log has no required fields
  return {
    flows: [flow],
    connectors: [{ id: "c0", name: "clock", type: "cron", settings: {} }],
    processors: [],
    env: [],
  };
}

describe("validateDocument", () => {
  it("accepts a complete cron→log flow", () => {
    expect(validateDocument(runnableDoc())).toEqual({ ok: true, issues: [] });
  });

  it("rejects a document with no flows", () => {
    const result = validateDocument(blankDocument());
    expect(result.ok).toBe(false);
    expect(issueMessages(result)).toContain("Add at least one flow to run.");
  });

  it("flags a missing required setting", () => {
    const flow = emptyFlow("greet");
    flow.process = [newBlock("set-payload")]; // `value` is required, no default
    const result = validateDocument({ flows: [flow], connectors: [], processors: [], env: [] });
    expect(result.ok).toBe(false);
    expect(issueMessages(result).some((i) => i.includes("Value is required"))).toBe(true);
  });

  // A connector type can come from the environment, which the runtime substitutes
  // across the whole document before decoding. The editor has no environment, so
  // reporting it as unknown would block Run and Deploy on a config that loads fine.
  it("accepts a connector type that is an environment placeholder", () => {
    const doc = runnableDoc();
    doc.connectors[0].type = "${CLOCK_CONNECTOR_TYPE}";

    expect(validateDocument(doc)).toEqual({ ok: true, issues: [] });
  });

  it("still flags a connector type that is merely unknown", () => {
    const doc = runnableDoc();
    doc.connectors[0].type = "not-a-connector";

    const result = validateDocument(doc);
    expect(result.ok).toBe(false);
    expect(
      issueMessages(result).some((i) => i.includes('unknown connector type')),
    ).toBe(true);
  });

  // An unrelated placeholder connection is not a candidate for an implicit source
  // binding: it might resolve to anything, so counting it as a second cron would
  // demand a choice the author does not have to make.
  it("an unrelated placeholder does not make the implicit source binding ambiguous", () => {
    const doc = runnableDoc();
    doc.flows[0].source!.connectorRef = "";
    doc.connectors.push({
      id: "c1",
      name: "llm",
      type: "${LLM_CONNECTOR_TYPE}",
      settings: {},
    });

    expect(validateDocument(doc)).toEqual({ ok: true, issues: [] });
  });

  // Two connections of the type the source names is still ambiguous.
  it("still flags two connections of the source's own type", () => {
    const doc = runnableDoc();
    doc.flows[0].source!.connectorRef = "";
    doc.connectors.push({ id: "c1", name: "clock2", type: "cron", settings: {} });

    const result = validateDocument(doc);
    expect(result.ok).toBe(false);
    expect(issueMessages(result).some((i) => i.includes("choose one"))).toBe(true);
  });

  // Only a bare placeholder is exempt. Anything else is a type name that happens to
  // contain a brace, and pretending otherwise would let real typos through.
  it("does not exempt a type that merely contains a placeholder", () => {
    const doc = runnableDoc();
    doc.connectors[0].type = "llm-${PROVIDER}";

    const result = validateDocument(doc);
    expect(result.ok).toBe(false);
  });

  it("flags a dangling connector reference", () => {
    const rest = newBlock("rest");
    rest.settings.connector = "nope";
    const flow = emptyFlow("caller");
    flow.process = [rest];
    const result = validateDocument({ flows: [flow], connectors: [], processors: [], env: [] });
    expect(result.ok).toBe(false);
    expect(issueMessages(result).some((i) => i.includes('"nope"'))).toBe(true);
  });

  it("resolves a connector reference that exists", () => {
    const rest = newBlock("rest");
    rest.settings.connector = "api";
    const flow = emptyFlow("caller");
    flow.process = [rest];
    const result = validateDocument({
      flows: [flow],
      connectors: [
        { id: "c1", name: "api", type: "http-client", settings: { baseURL: "http://x" } },
      ],
      processors: [],
      env: [],
    });
    expect(result).toEqual({ ok: true, issues: [] });
  });

  it("flags an empty required connector reference", () => {
    const rest = newBlock("rest");
    rest.settings.connector = "";
    const flow = emptyFlow("caller");
    flow.process = [rest];
    const result = validateDocument({ flows: [flow], connectors: [], processors: [], env: [] });
    expect(result.ok).toBe(false);
    expect(issueMessages(result).some((i) => i.includes("Connector is required"))).toBe(true);
  });

  it("flags a dangling flow reference", () => {
    const ref = newBlock("flow-ref");
    ref.settings.flow = "ghost";
    const flow = emptyFlow("caller");
    flow.process = [ref];
    const result = validateDocument({ flows: [flow], connectors: [], processors: [], env: [] });
    expect(result.ok).toBe(false);
    expect(issueMessages(result).some((i) => i.includes('"ghost"'))).toBe(true);
  });

  it("allows an unbound source when a single connector of the type exists", () => {
    const doc = runnableDoc();
    doc.flows[0].source!.connectorRef = undefined; // lone cron connector binds implicitly
    expect(validateDocument(doc)).toEqual({ ok: true, issues: [] });
  });

  it("requires choosing among multiple connectors of the same type", () => {
    const doc = runnableDoc();
    doc.flows[0].source!.connectorRef = undefined;
    doc.connectors.push({ id: "c1", name: "clock-2", type: "cron", settings: {} });
    const result = validateDocument(doc);
    expect(result.ok).toBe(false);
    expect(issueMessages(result).some((i) => i.includes("choose one"))).toBe(true);
  });

  it("flags an explicit source binding that doesn't resolve", () => {
    const doc = runnableDoc();
    doc.flows[0].source!.connectorRef = "ghost";
    const result = validateDocument(doc);
    expect(result.ok).toBe(false);
    expect(issueMessages(result).some((i) => i.includes('"ghost"'))).toBe(true);
  });

  it("flags duplicate connection names", () => {
    const doc = runnableDoc();
    doc.connectors.push(
      { id: "a", name: "db", type: "database", settings: { driver: "sqlite", dsn: "x" } },
      { id: "b", name: "db", type: "database", settings: { driver: "sqlite", dsn: "y" } },
    );
    const result = validateDocument(doc);
    expect(result.ok).toBe(false);
    expect(issueMessages(result).some((i) => i.includes("used more than once"))).toBe(true);
  });

  it("flags an empty required branch and missing condition on a composite", () => {
    const ifb = newBlock("if"); // seeds empty then/else sub-flows, no condition
    const flow = emptyFlow("router");
    flow.process = [ifb];
    const result = validateDocument({ flows: [flow], connectors: [], processors: [], env: [] });
    expect(result.ok).toBe(false);
    expect(issueMessages(result).some((i) => i.includes("Condition is required"))).toBe(true);
    expect(issueMessages(result).some((i) => i.includes("needs at least one step"))).toBe(true);
  });

  it("accepts a composite once its condition and required branch are filled", () => {
    const ifb = newBlock("if");
    ifb.settings.condition = "body.ok";
    const branch = emptyFlow("");
    branch.process = [newBlock("log")];
    ifb.slots!.then = [branch];
    const flow = emptyFlow("router");
    flow.process = [ifb];
    expect(validateDocument({ flows: [flow], connectors: [], processors: [], env: [] })).toEqual({
      ok: true,
      issues: [],
    });
  });

  // The ids are what let the Problems tab select the offending node when an issue is
  // clicked; without them a problem can only be read, not navigated to.
  describe("issue identity", () => {
    it("points a block's issue at that block and its flow", () => {
      const block = newBlock("set-payload"); // `value` is required, no default
      const flow = emptyFlow("greet");
      flow.process = [block];

      const result = validateDocument({ flows: [flow], connectors: [], processors: [], env: [] });
      const issue = result.issues.find((i) => i.message.includes("Value is required"));

      expect(issue).toMatchObject({
        severity: "error",
        flowId: flow.id,
        blockId: block.id,
      });
    });

    // A block nested in a composite's branch must still be selectable — the point is
    // to reach the block at fault, not the composite that happens to contain it.
    it("points a nested block's issue at the nested block", () => {
      const inner = newBlock("set-payload");
      const branch = emptyFlow("");
      branch.process = [inner];
      const ifb = newBlock("if");
      ifb.settings.condition = "body.ok";
      ifb.slots!.then = [branch];
      const flow = emptyFlow("router");
      flow.process = [ifb];

      const result = validateDocument({ flows: [flow], connectors: [], processors: [], env: [] });
      const issue = result.issues.find((i) => i.message.includes("Value is required"));

      expect(issue?.blockId).toBe(inner.id);
      expect(issue?.flowId).toBe(branch.id);
    });

    it("leaves a document-wide issue unanchored", () => {
      const result = validateDocument(blankDocument());
      const issue = result.issues.find((i) => i.message === "Add at least one flow to run.");

      expect(issue?.flowId).toBeUndefined();
      expect(issue?.blockId).toBeUndefined();
    });

    // A connection is not a canvas node, so clicking its issue has nothing to select.
    it("leaves a connection issue unanchored", () => {
      const result = validateDocument({
        flows: [],
        connectors: [{ id: "c0", name: "api", type: "rest", settings: {} }],
        processors: [],
        env: [],
      });
      const issue = result.issues.find((i) => i.message.startsWith('Connection "api"'));

      expect(issue).toBeDefined();
      expect(issue?.blockId).toBeUndefined();
    });

    it("anchors a flow's source issue to the flow", () => {
      const flow = emptyFlow("ticker");
      flow.source = { connector: "cron", type: "cron", settings: {} }; // no schedule
      const result = validateDocument({ flows: [flow], connectors: [], processors: [], env: [] });
      const issue = result.issues.find((i) => i.message.includes("source"));

      expect(issue?.flowId).toBe(flow.id);
    });
  });
});
