import { describe, it, expect } from "vitest";
import { emptyDocument, newBlock } from "./document";
import { fromConfig, toConfig } from "./serialize";

describe("serialize", () => {
  it("maps a flow's leaf blocks to the runtime config shape", () => {
    const doc = emptyDocument();
    doc.flows[0].name = "demo";
    const log = newBlock("log");
    log.name = "say-hi";
    doc.flows[0].process = [log];

    const config = toConfig(doc);
    expect(config.flows).toHaveLength(1);
    expect(config.flows![0].name).toBe("demo");
    expect(config.flows![0].process![0].type).toBe("log");
    expect(config.flows![0].process![0].name).toBe("say-hi");
    expect(config.flows![0].process![0].settings).toMatchObject({ level: "info" });
  });

  it("omits empty settings and absent source", () => {
    const doc = emptyDocument();
    doc.flows[0].process = [{ id: "x", type: "noop", settings: {} }];
    const block = toConfig(doc).flows![0].process![0];
    expect(block.settings).toBeUndefined();
    expect(toConfig(doc).flows![0].source).toBeUndefined();
  });

  it("round-trips a flow's source", () => {
    const doc = emptyDocument();
    doc.flows[0].source = {
      connector: "cron",
      type: "cron",
      settings: { schedule: "@every 2s" },
    };

    const config = toConfig(doc);
    expect(config.flows![0].source).toMatchObject({
      connector: "cron",
      type: "cron",
      settings: { schedule: "@every 2s" },
    });

    const restored = fromConfig(config);
    expect(restored.flows[0].source).toMatchObject({
      connector: "cron",
      type: "cron",
      settings: { schedule: "@every 2s" },
    });
  });

  it("round-trips through fromConfig with fresh ids", () => {
    const doc = emptyDocument();
    doc.flows[0].name = "demo";
    doc.flows[0].process = [newBlock("set-payload")];

    const restored = fromConfig(toConfig(doc));
    expect(restored.flows).toHaveLength(1);
    expect(restored.flows[0].name).toBe("demo");
    expect(restored.flows[0].process[0].type).toBe("set-payload");
    expect(restored.flows[0].process[0].id).not.toBe(doc.flows[0].process[0].id);
  });

  it("falls back to an empty document for a config with no flows", () => {
    expect(fromConfig({}).flows).toHaveLength(1);
  });

  it("round-trips connector instances (connections)", () => {
    const doc = emptyDocument();
    doc.connectors = [
      { id: "a", name: "primary-db", type: "database", settings: { dsn: "x" } },
      { id: "b", name: "no-settings", type: "cron", settings: {} },
    ];

    const config = toConfig(doc);
    expect(config.connectors).toEqual([
      { name: "primary-db", type: "database", settings: { dsn: "x" } },
      { name: "no-settings", type: "cron" }, // empty settings omitted
    ]);

    const restored = fromConfig(config);
    expect(restored.connectors).toHaveLength(2);
    expect(restored.connectors[0]).toMatchObject({
      name: "primary-db",
      type: "database",
      settings: { dsn: "x" },
    });
    expect(restored.connectors[0].id).not.toBe("a"); // fresh client id
  });

  it("round-trips a source bound to a connector instance", () => {
    const doc = emptyDocument();
    doc.connectors = [
      { id: "c1", name: "main-http", type: "http", settings: {} },
    ];
    doc.flows[0].source = {
      connector: "http", // editor-only connector type
      connectorRef: "main-http", // bound instance name
      type: "http",
      settings: {},
    };

    // The runtime gets the instance name under `connector`.
    const config = toConfig(doc);
    expect(config.flows![0].source).toMatchObject({
      connector: "main-http",
      type: "http",
    });

    // On the way back, the connector type is recovered from the instance.
    const restored = fromConfig(config);
    expect(restored.flows[0].source).toMatchObject({
      connector: "http",
      connectorRef: "main-http",
      type: "http",
    });
  });

  it("keeps connectors even when the config has no flows", () => {
    const doc = fromConfig({
      connectors: [{ name: "c1", type: "cron", settings: {} }],
    });
    expect(doc.connectors).toHaveLength(1);
    expect(doc.connectors[0].name).toBe("c1");
  });

  it("maps composite slots and scalars to runtime keys", () => {
    const doc = emptyDocument();
    const branch = newBlock("if"); // seeds then/else sub-flows + condition
    branch.settings.condition = "body.ok";
    branch.slots!.then[0].process = [newBlock("log")];
    doc.flows[0].process = [branch];

    const block = toConfig(doc).flows![0].process![0];
    expect(block.condition).toBe("body.ok");
    expect(block.settings).toBeUndefined(); // scalars are lifted, not in settings
    expect(block.then!.process![0].type).toBe("log");
    expect(block.else!.process).toEqual([]);
  });

  it("round-trips a composite back into slots", () => {
    const doc = emptyDocument();
    const branch = newBlock("if");
    branch.settings.condition = "x > 1";
    branch.slots!.then[0].process = [newBlock("set-payload")];
    doc.flows[0].process = [branch];

    const restored = fromConfig(toConfig(doc));
    const node = restored.flows[0].process[0];
    expect(node.type).toBe("if");
    expect(node.settings.condition).toBe("x > 1");
    expect(node.slots!.then[0].process[0].type).toBe("set-payload");
  });
});
