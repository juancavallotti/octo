import { describe, it, expect } from "vitest";
import {
  type BlockNode,
  type EditorDocument,
  type FlowDoc,
  emptyFlow,
  newBlock,
  withErrorChain,
} from "../model/document";
import { planBreakpoint } from "./breakpoint";

/** A document holding one top-level flow. */
function docWith(flow: FlowDoc): EditorDocument {
  return { flows: [flow], connectors: [], processors: [], env: [] };
}

/** A named leaf block. */
function block(type: string, name?: string): BlockNode {
  const b = newBlock(type);
  if (name !== undefined) b.name = name;
  return b;
}

/** A sub-flow with a name and one step. */
function branch(name: string, step: BlockNode): FlowDoc {
  const f = emptyFlow(name);
  f.process = [step];
  return f;
}

describe("planBreakpoint", () => {
  it("addresses a block in the flow's own chain", () => {
    const target = block("log", "audit");
    const flow = emptyFlow("orders");
    flow.process = [block("set-payload", "prep"), target];

    const plan = planBreakpoint(docWith(flow), target.id);

    expect(plan?.flow).toBe("orders");
    expect(plan?.address).toBe("orders.audit");
    expect(plan?.label).toBe("audit");
  });

  // An unnamed block is still addressable by its type, as long as it is the only one.
  it("addresses an unnamed block by its type", () => {
    const target = block("log");
    const flow = emptyFlow("orders");
    flow.process = [target];

    expect(planBreakpoint(docWith(flow), target.id)?.address).toBe("orders.log");
  });

  it("addresses a block on the flow's error chain", () => {
    const target = block("log", "notify");
    const flow = withErrorChain(emptyFlow("orders"));
    flow.process = [block("set-payload", "prep")];
    flow.error!.process = [target];

    expect(planBreakpoint(docWith(flow), target.id)?.address).toBe("orders[error].notify");
  });

  it("returns null for a block that isn't in the document", () => {
    const flow = emptyFlow("orders");
    flow.process = [block("log")];
    expect(planBreakpoint(docWith(flow), "nope")).toBeNull();
  });

  describe("descending into composites", () => {
    it("addresses a block in an if's else branch", () => {
      const target = block("log", "reject");
      const iff = newBlock("if");
      iff.slots!.else = [branch("", target)];
      const flow = emptyFlow("orders");
      flow.process = [iff];

      expect(planBreakpoint(docWith(flow), target.id)?.address).toBe(
        "orders.if[else].reject",
      );
    });

    it("addresses a block in a foreach body", () => {
      const target = block("log", "each");
      const loop = newBlock("foreach");
      loop.slots!.body = [branch("", target)];
      const flow = emptyFlow("orders");
      flow.process = [loop];

      expect(planBreakpoint(docWith(flow), target.id)?.address).toBe(
        "orders.foreach[body].each",
      );
    });

    it("addresses a block on a handle-errors error path", () => {
      const target = block("log", "notify");
      const guard = newBlock("handle-errors");
      guard.slots!.error = [branch("", target)];
      const flow = emptyFlow("orders");
      flow.process = [guard];

      expect(planBreakpoint(docWith(flow), target.id)?.address).toBe(
        "orders.handle-errors[error].notify",
      );
    });

    // A list-valued branch (fork/switch/router/agent) is addressed by the member's name.
    it("addresses a fork branch by its name", () => {
      const target = block("log", "log-it");
      const fork = newBlock("fork");
      fork.slots!.branches = [branch("primary", block("log", "other")), branch("audit", target)];
      const flow = emptyFlow("orders");
      flow.process = [fork];

      expect(planBreakpoint(docWith(flow), target.id)?.address).toBe(
        "orders.fork[audit].log-it",
      );
    });

    it("falls back to the index when the branch has no name", () => {
      const target = block("log", "log-it");
      const fork = newBlock("fork");
      fork.slots!.branches = [branch("", block("log", "other")), branch("", target)];
      const flow = emptyFlow("orders");
      flow.process = [fork];

      expect(planBreakpoint(docWith(flow), target.id)?.address).toBe(
        "orders.fork[1].log-it",
      );
    });

    it("falls back to the index when two branches share a name", () => {
      const target = block("log", "log-it");
      const fork = newBlock("fork");
      fork.slots!.branches = [branch("dup", block("log", "other")), branch("dup", target)];
      const flow = emptyFlow("orders");
      flow.process = [fork];

      expect(planBreakpoint(docWith(flow), target.id)?.address).toBe(
        "orders.fork[1].log-it",
      );
    });

    it("addresses a block nested two composites deep", () => {
      const target = block("log", "deep");
      const inner = newBlock("switch");
      inner.slots!.cases = [branch("vip", target)];
      const outer = newBlock("foreach");
      outer.slots!.body = [branch("", inner)];
      const flow = emptyFlow("orders");
      flow.process = [outer];

      expect(planBreakpoint(docWith(flow), target.id)?.address).toBe(
        "orders.foreach[body].switch[vip].deep",
      );
    });
  });

  describe("making an ambiguous path addressable", () => {
    // Two unnamed `log` blocks both answer to "log" and the runtime refuses to guess.
    // Rather than make the user name one, we name it ourselves — in the throwaway clone.
    it("synthesizes a name when siblings share a label", () => {
      const first = block("log");
      const target = block("log");
      const flow = emptyFlow("orders");
      flow.process = [first, target];

      const plan = planBreakpoint(docWith(flow), target.id)!;

      expect(plan.address).toBe("orders.__bp_1");
      // The name exists in the clone we will invoke...
      const cloned = plan.doc.flows[0].process.find((b) => b.id === target.id);
      expect(cloned?.name).toBe("__bp_1");
      // ...and the block keeps its friendly label for the UI.
      expect(plan.label).toBe("log");
    });

    // The saved document must never be mutated — the user did not ask us to rename it.
    it("leaves the original document untouched", () => {
      const target = block("log");
      const flow = emptyFlow("orders");
      flow.process = [block("log"), target];
      const doc = docWith(flow);
      const before = structuredClone(doc);

      planBreakpoint(doc, target.id);

      expect(doc).toEqual(before);
    });

    it("synthesizes a name for a composite ancestor that is ambiguous too", () => {
      const target = block("log", "inner");
      const first = newBlock("if");
      const second = newBlock("if");
      second.slots!.then = [branch("", target)];
      const flow = emptyFlow("orders");
      flow.process = [first, second]; // two `if` blocks: neither addressable by type

      expect(planBreakpoint(docWith(flow), target.id)?.address).toBe(
        "orders.__bp_1[then].inner",
      );
    });

    // A name carrying `.`/`[`/`]` would be mis-parsed by the address grammar.
    it("synthesizes a name when the block's own name would break the grammar", () => {
      const target = block("log", "a.b");
      const flow = emptyFlow("orders");
      flow.process = [target];

      expect(planBreakpoint(docWith(flow), target.id)?.address).toBe("orders.__bp_1");
    });

    it("does not collide with a block already named like a synthetic one", () => {
      const target = block("log");
      const flow = emptyFlow("orders");
      flow.process = [block("log"), block("noop", "__bp_1"), target];

      expect(planBreakpoint(docWith(flow), target.id)?.address).toBe("orders.__bp_2");
    });
  });

  describe("refusing to mis-address", () => {
    // The runtime matches member names before indices. If we had to address a branch
    // by position while a sibling is *named* that number, the address would silently
    // select the wrong branch — so refuse, and the caller hides the button.
    it("returns null when an index would collide with a sibling's name", () => {
      const target = block("log", "log-it");
      const fork = newBlock("fork");
      fork.slots!.branches = [branch("1", block("log", "other")), branch("", target)];
      const flow = emptyFlow("orders");
      flow.process = [fork];

      expect(planBreakpoint(docWith(flow), target.id)).toBeNull();
    });

    // A flow's name is not ours to rewrite — `flow-ref` blocks address flows by name.
    it("returns null when the flow's name would break the grammar", () => {
      const target = block("log", "audit");
      const flow = emptyFlow("orders.eu");
      flow.process = [target];

      expect(planBreakpoint(docWith(flow), target.id)).toBeNull();
    });

    it("returns null when the flow has no name", () => {
      const target = block("log", "audit");
      const flow = emptyFlow("");
      flow.process = [target];

      expect(planBreakpoint(docWith(flow), target.id)).toBeNull();
    });
  });
});
