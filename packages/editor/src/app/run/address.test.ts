import { describe, it, expect } from "vitest";
import {
  type BlockNode,
  type EditorDocument,
  type FlowDoc,
  emptyFlow,
  newBlock,
  withErrorChain,
} from "../model/document";
import {
  blockIdAddresses,
  debugConflict,
  namesNeededFor,
  naturalAddress,
  planBreakpoint,
} from "./address";

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

describe("naturalAddress", () => {
  it("addresses a block by its name", () => {
    const target = block("log", "audit");
    const flow = emptyFlow("orders");
    flow.process = [block("set-payload", "prep"), target];

    expect(naturalAddress(docWith(flow), target.id)).toBe("orders.audit");
  });

  it("falls back to the type when the block is unnamed and alone in its chain", () => {
    const target = block("log");
    const flow = emptyFlow("orders");
    flow.process = [block("set-payload"), target];

    expect(naturalAddress(docWith(flow), target.id)).toBe("orders.log");
  });

  it("descends into a branch", () => {
    const target = block("log", "inner");
    const gate = newBlock("if");
    gate.name = "check";
    gate.slots!.then = [branch("", target)];
    const flow = emptyFlow("orders");
    flow.process = [gate];

    expect(naturalAddress(docWith(flow), target.id)).toBe("orders.check[then].inner");
  });

  it("addresses the error chain", () => {
    const target = block("log", "notify");
    const flow = withErrorChain(emptyFlow("orders"));
    flow.error!.process = [target];

    expect(naturalAddress(docWith(flow), target.id)).toBe("orders[error].notify");
  });

  /**
   * The whole difference from planBreakpoint. A breakpoint may invent a name because it
   * is thrown away with the run; a mock's address is written to a file and has to be
   * found again on the next reload, when every client id is new. So it invents nothing,
   * and says so.
   */
  describe("inventing nothing", () => {
    it("returns null when a sibling answers to the same label", () => {
      const target = block("log");
      const flow = emptyFlow("orders");
      flow.process = [block("log"), target];

      expect(naturalAddress(docWith(flow), target.id)).toBeNull();
    });

    it("returns null when the block's name would break the grammar", () => {
      const target = block("log", "a.b");
      const flow = emptyFlow("orders");
      flow.process = [target];

      expect(naturalAddress(docWith(flow), target.id)).toBeNull();
    });

    // An ambiguous composite makes everything under it unaddressable too.
    it("returns null when an ancestor on the path is ambiguous", () => {
      const target = block("log", "inner");
      const first = newBlock("if");
      const second = newBlock("if");
      second.slots!.then = [branch("", target)];
      const flow = emptyFlow("orders");
      flow.process = [first, second];

      expect(naturalAddress(docWith(flow), target.id)).toBeNull();
    });

    it("returns null for a block that is not in the document", () => {
      expect(naturalAddress(docWith(emptyFlow("orders")), "nope")).toBeNull();
    });
  });
});

describe("namesNeededFor", () => {
  // Nothing to do: the block already has a durable address, so placing a mock on it must
  // not touch the document.
  it("asks for no rename when the block is already addressable", () => {
    const target = block("log", "audit");
    const flow = emptyFlow("orders");
    flow.process = [target];

    expect(namesNeededFor(docWith(flow), target.id)).toEqual([]);
  });

  it("names an ambiguous block after its type", () => {
    const target = block("log");
    const flow = emptyFlow("orders");
    flow.process = [block("log"), target];

    expect(namesNeededFor(docWith(flow), target.id)).toEqual([
      { blockId: target.id, name: "log-2" },
    ]);
  });

  it("picks a name no sibling already holds", () => {
    const target = block("log");
    const flow = emptyFlow("orders");
    flow.process = [block("log"), block("noop", "log-2"), target];

    expect(namesNeededFor(docWith(flow), target.id)).toEqual([
      { blockId: target.id, name: "log-3" },
    ]);
  });

  /**
   * The rename can land on a block the user never clicked. Here the target is perfectly
   * addressable within its own branch — it is the ambiguous `if` *above* it that leaves it
   * with no address, so that is what gets named.
   */
  it("names an ambiguous composite on the path rather than the target", () => {
    const target = block("log", "inner");
    const first = newBlock("if");
    const second = newBlock("if");
    second.slots!.then = [branch("", target)];
    const flow = emptyFlow("orders");
    flow.process = [first, second];
    const doc = docWith(flow);

    expect(namesNeededFor(doc, target.id)).toEqual([{ blockId: second.id, name: "if-2" }]);

    second.name = "if-2";
    expect(naturalAddress(doc, target.id)).toBe("orders.if-2[then].inner");
  });

  // Both levels can need one at once: an ambiguous composite AND an ambiguous target.
  it("names every ambiguous block on the path", () => {
    const target = block("log");
    const first = newBlock("if");
    const second = newBlock("if");
    const sub = branch("", target);
    sub.process = [block("log"), target]; // the target is ambiguous in here too
    second.slots!.then = [sub];
    const flow = emptyFlow("orders");
    flow.process = [first, second];

    expect(namesNeededFor(docWith(flow), target.id)).toEqual([
      { blockId: second.id, name: "if-2" },
      { blockId: target.id, name: "log-2" },
    ]);
  });

  // Naming cannot rescue an unaddressable branch or flow, so the caller must not offer
  // the affordance at all — exactly as run-to-here already hides itself.
  it("returns null when no name could make the block addressable", () => {
    const target = block("log", "log-it");
    const fork = newBlock("fork");
    fork.slots!.branches = [branch("1", block("log", "other")), branch("", target)];
    const flow = emptyFlow("orders");
    flow.process = [fork];

    expect(namesNeededFor(docWith(flow), target.id)).toBeNull();
  });

  // The renames, once applied, must actually yield an address. This is the contract the
  // UI leans on: rename, then read the address back.
  it("yields a block that naturalAddress can then address", () => {
    const target = block("log");
    const flow = emptyFlow("orders");
    flow.process = [block("log"), target];
    const doc = docWith(flow);

    for (const rename of namesNeededFor(doc, target.id)!) {
      const found = flow.process.find((b) => b.id === rename.blockId)!;
      found.name = rename.name;
    }

    expect(naturalAddress(doc, target.id)).toBe("orders.log-2");
  });
});

describe("blockIdAddresses", () => {
  it("maps every addressable block, at any depth, and omits the rest", () => {
    const named = block("log", "audit");
    const inner = block("log", "inner");
    const gate = newBlock("if");
    gate.name = "check";
    gate.slots!.then = [branch("", inner)];
    const twinA = block("noop");
    const twinB = block("noop"); // ambiguous: neither is addressable
    const flow = emptyFlow("orders");
    flow.process = [named, gate, twinA, twinB];

    const map = blockIdAddresses(docWith(flow));

    expect(map.get(named.id)).toBe("orders.audit");
    expect(map.get(gate.id)).toBe("orders.check");
    expect(map.get(inner.id)).toBe("orders.check[then].inner");
    expect(map.has(twinA.id)).toBe(false);
    expect(map.has(twinB.id)).toBe(false);
  });
});

describe("debugConflict", () => {
  /**
   * A mock deletes the subtree it replaces, so anything addressed inside it can never
   * fire. The runtime rejects the run outright; its own error reads like a typo, so the
   * editor says what actually happened.
   */
  it("reports a spy addressed inside a mocked block", () => {
    const conflict = debugConflict(["orders.fanout"], ["orders.fanout[audit].log-it"]);
    expect(conflict).toContain("inside the mocked block");
    expect(conflict).toContain("orders.fanout[audit].log-it");
  });

  it("reports a breakpoint inside a mocked block", () => {
    expect(debugConflict(["orders.charge"], ["orders.charge.inner"])).not.toBeNull();
  });

  it("allows a spy on the mocked block itself — it wraps the mock, it is not inside it", () => {
    expect(debugConflict(["orders.charge"], ["orders.charge"])).toBeNull();
  });

  // The separator is what makes this safe: `orders.fan` is a different block entirely.
  it("does not mistake a shared prefix for containment", () => {
    expect(debugConflict(["orders.fan"], ["orders.fanout"])).toBeNull();
  });

  it("returns null when nothing is mocked", () => {
    expect(debugConflict([], ["orders.charge"])).toBeNull();
  });
});
