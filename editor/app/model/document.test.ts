import { describe, it, expect } from "vitest";
import { emptyDocument, findBlock, findFlow, newBlock } from "./document";

describe("document tree helpers", () => {
  it("finds a block and a flow nested inside a composite slot", () => {
    const doc = emptyDocument();
    const branch = newBlock("if"); // seeds then/else sub-flows
    const nested = newBlock("log");
    branch.slots!.then[0].process = [nested];
    doc.flows[0].process = [branch];

    expect(findBlock(doc, nested.id)?.type).toBe("log");
    expect(findBlock(doc, branch.id)?.type).toBe("if");
    expect(findFlow(doc, branch.slots!.then[0].id)?.id).toBe(
      branch.slots!.then[0].id,
    );
  });

  it("returns undefined for an unknown id", () => {
    expect(findBlock(emptyDocument(), "nope")).toBeUndefined();
    expect(findFlow(emptyDocument(), "nope")).toBeUndefined();
  });
});
