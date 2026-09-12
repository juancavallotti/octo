import { describe, expect, it } from "vitest";
import { emptyDocument, type BlockNode, type EditorDocument, type FlowDoc } from "../model/document";
import { isPureFlow, PURE_BLOCKS } from "./pure";

let seq = 0;
const id = () => `id-${++seq}`;

function block(type: string, settings: Record<string, unknown> = {}, slots?: Record<string, FlowDoc[]>): BlockNode {
  return { id: id(), type, settings, ...(slots ? { slots } : {}) };
}

function flow(name: string, process: BlockNode[]): FlowDoc {
  return { id: id(), name, process };
}

function docOf(...flows: FlowDoc[]): EditorDocument {
  return { ...emptyDocument(), flows };
}

describe("isPureFlow", () => {
  it("clears a flow that only transforms the message", () => {
    const f = flow("clean", [block("set-variable", { as: "x" }), block("set-payload")]);
    expect(isPureFlow(docOf(f), f.id)).toBe(true);
  });

  it("refuses a flow that calls out", () => {
    const f = flow("calls", [block("set-variable"), block("rest", { connector: "api" })]);
    expect(isPureFlow(docOf(f), f.id)).toBe(false);
  });

  it("refuses a block type it has never heard of, rather than assuming", () => {
    const f = flow("new", [block("some-block-added-next-year")]);
    expect(isPureFlow(docOf(f), f.id)).toBe(false);
  });

  it("refuses a block that writes where a later run could read it", () => {
    // Never leaves the machine, still leaves a trace: the reason the list is about
    // observable effects and not about the network.
    const f = flow("writes", [block("object-write", { key: "k" })]);
    expect(isPureFlow(docOf(f), f.id)).toBe(false);
  });

  it("looks inside a composite's slots", () => {
    const f = flow("outer", [
      block("if", { condition: "true" }, { then: [flow("then", [block("rest")])] }),
    ]);
    expect(isPureFlow(docOf(f), f.id)).toBe(false);
  });

  it("clears a composite whose slots are clean", () => {
    const f = flow("outer", [
      block("if", { condition: "true" }, { then: [flow("then", [block("set-payload")])] }),
    ]);
    expect(isPureFlow(docOf(f), f.id)).toBe(true);
  });

  it("follows a flow-ref into its target", () => {
    const target = flow("helper", [block("slack-send-message")]);
    const caller = flow("caller", [block("flow-ref", { flow: "helper" })]);
    expect(isPureFlow(docOf(caller, target), caller.id)).toBe(false);
  });

  it("clears a flow-ref whose target is clean", () => {
    const target = flow("helper", [block("set-payload")]);
    const caller = flow("caller", [block("flow-ref", { flow: "helper" })]);
    expect(isPureFlow(docOf(caller, target), caller.id)).toBe(true);
  });

  it("refuses a flow-ref cycle rather than recursing forever", () => {
    const a = flow("a", [block("flow-ref", { flow: "b" })]);
    const b = flow("b", [block("flow-ref", { flow: "a" })]);
    expect(isPureFlow(docOf(a, b), a.id)).toBe(false);
  });

  it("refuses a flow-ref with no resolvable target", () => {
    const caller = flow("caller", [block("flow-ref", { flow: "gone" })]);
    expect(isPureFlow(docOf(caller), caller.id)).toBe(false);
  });

  it("judges the error chain too, since a background run can reach it", () => {
    const f = flow("guarded", [block("validate")]);
    f.error = { id: id(), name: "error", process: [block("slack-send-message")] };
    expect(isPureFlow(docOf(f), f.id)).toBe(false);
  });

  it("refuses a flow it cannot find", () => {
    expect(isPureFlow(docOf(), "nope")).toBe(false);
  });

  it("does not list a block that reaches the network", () => {
    // A guard on the list itself: these are the ones it would be most costly to add
    // by accident, and the ones a future edit is most likely to reach for.
    for (const type of ["rest", "sql", "llm", "slack-send-message", "cli-run", "queue-dispatch"]) {
      expect(PURE_BLOCKS.has(type)).toBe(false);
    }
  });
});
