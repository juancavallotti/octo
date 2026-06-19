import { describe, it, expect } from "vitest";
import {
  EditorDocument,
  blankDocument,
  emptyFlow,
  newBlock,
} from "./document";
import { validateDocument } from "./validate";

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
  };
}

describe("validateDocument", () => {
  it("accepts a complete cron→log flow", () => {
    expect(validateDocument(runnableDoc())).toEqual({ ok: true, issues: [] });
  });

  it("rejects a document with no flows", () => {
    const result = validateDocument(blankDocument());
    expect(result.ok).toBe(false);
    expect(result.issues).toContain("Add at least one flow to run.");
  });

  it("flags a missing required setting", () => {
    const flow = emptyFlow("greet");
    flow.process = [newBlock("set-payload")]; // `value` is required, no default
    const result = validateDocument({ flows: [flow], connectors: [], processors: [] });
    expect(result.ok).toBe(false);
    expect(result.issues.some((i) => i.includes("Value is required"))).toBe(true);
  });

  it("flags a dangling connector reference", () => {
    const rest = newBlock("rest");
    rest.settings.connector = "nope";
    const flow = emptyFlow("caller");
    flow.process = [rest];
    const result = validateDocument({ flows: [flow], connectors: [], processors: [] });
    expect(result.ok).toBe(false);
    expect(result.issues.some((i) => i.includes('"nope"'))).toBe(true);
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
    });
    expect(result).toEqual({ ok: true, issues: [] });
  });

  it("flags an empty required connector reference", () => {
    const rest = newBlock("rest");
    rest.settings.connector = "";
    const flow = emptyFlow("caller");
    flow.process = [rest];
    const result = validateDocument({ flows: [flow], connectors: [], processors: [] });
    expect(result.ok).toBe(false);
    expect(result.issues.some((i) => i.includes("Connector is required"))).toBe(true);
  });

  it("flags a dangling flow reference", () => {
    const ref = newBlock("flow-ref");
    ref.settings.flow = "ghost";
    const flow = emptyFlow("caller");
    flow.process = [ref];
    const result = validateDocument({ flows: [flow], connectors: [], processors: [] });
    expect(result.ok).toBe(false);
    expect(result.issues.some((i) => i.includes('"ghost"'))).toBe(true);
  });

  it("flags an unbound flow source", () => {
    const doc = runnableDoc();
    doc.flows[0].source!.connectorRef = undefined;
    const result = validateDocument(doc);
    expect(result.ok).toBe(false);
    expect(result.issues.some((i) => i.includes("needs a connection"))).toBe(true);
  });

  it("flags duplicate connection names", () => {
    const doc = runnableDoc();
    doc.connectors.push(
      { id: "a", name: "db", type: "database", settings: { driver: "sqlite", dsn: "x" } },
      { id: "b", name: "db", type: "database", settings: { driver: "sqlite", dsn: "y" } },
    );
    const result = validateDocument(doc);
    expect(result.ok).toBe(false);
    expect(result.issues.some((i) => i.includes("used more than once"))).toBe(true);
  });

  it("flags an empty required branch and missing condition on a composite", () => {
    const ifb = newBlock("if"); // seeds empty then/else sub-flows, no condition
    const flow = emptyFlow("router");
    flow.process = [ifb];
    const result = validateDocument({ flows: [flow], connectors: [], processors: [] });
    expect(result.ok).toBe(false);
    expect(result.issues.some((i) => i.includes("Condition is required"))).toBe(true);
    expect(result.issues.some((i) => i.includes("needs at least one step"))).toBe(true);
  });

  it("accepts a composite once its condition and required branch are filled", () => {
    const ifb = newBlock("if");
    ifb.settings.condition = "body.ok";
    const branch = emptyFlow("");
    branch.process = [newBlock("log")];
    ifb.slots!.then = [branch];
    const flow = emptyFlow("router");
    flow.process = [ifb];
    expect(validateDocument({ flows: [flow], connectors: [], processors: [] })).toEqual({
      ok: true,
      issues: [],
    });
  });
});
