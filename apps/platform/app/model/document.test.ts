import { describe, it, expect } from "vitest";
import {
  defaultSourceSettings,
  emptyDocument,
  findBlock,
  findFlow,
  newBlock,
} from "./document";

describe("document tree helpers", () => {
  it("finds a block and a flow nested in a flow's error path", () => {
    const doc = emptyDocument();
    const block = newBlock("log");
    doc.flows[0].error!.process = [block];

    expect(findBlock(doc, block.id)?.type).toBe("log");
    expect(findFlow(doc, doc.flows[0].error!.id)?.id).toBe(
      doc.flows[0].error!.id,
    );
  });

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

  it("seeds source settings from the schema field defaults", () => {
    // http route declares a maxBodyBytes default; cron declares none.
    expect(defaultSourceSettings("http", "http")).toMatchObject({
      maxBodyBytes: 1048576,
    });
    expect(defaultSourceSettings("cron", "cron")).toEqual({});
    expect(defaultSourceSettings("nope", "nope")).toEqual({});
  });
});
