import { describe, it, expect } from "vitest";
import { emptyDocument, newBlock } from "./document";
import { fromConfig, toConfig } from "./serialize";
import { validateDocument } from "./validate";

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

  it("binds a lone connector of the source's type when none is set explicitly", () => {
    const doc = emptyDocument();
    doc.connectors = [
      { id: "c0", name: "ticker", type: "cron", settings: {} },
    ];
    doc.flows[0].source = { connector: "cron", type: "cron", settings: {} };

    // No connectorRef, but the single cron connection ("ticker") binds implicitly.
    expect(toConfig(doc).flows![0].source!.connector).toBe("ticker");
  });

  it("falls back to the type name when no connector of the type exists", () => {
    const doc = emptyDocument();
    doc.flows[0].source = { connector: "cron", type: "cron", settings: {} };
    expect(toConfig(doc).flows![0].source!.connector).toBe("cron");
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

  it("does not invent a connectorRef for an implicit-default source", () => {
    const doc = emptyDocument();
    doc.flows[0].source = {
      connector: "cron",
      type: "cron",
      settings: { schedule: "@every 2s" },
    };

    // The type-name fallback ("cron") must not round-trip into an explicit
    // binding, which would read as a dangling reference and fail validation.
    const restored = fromConfig(toConfig(doc));
    expect(restored.flows[0].source?.connectorRef).toBeUndefined();
    expect(validateDocument(restored).ok).toBe(true);
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

  it("serializes handle-errors process/error as bare block lists", () => {
    const doc = emptyDocument();
    const he = newBlock("handle-errors"); // seeds process/error block-list slots
    he.slots!.process[0].process = [newBlock("rest")];
    he.slots!.error[0].process = [newBlock("set-payload")];
    doc.flows[0].process = [he];

    const block = toConfig(doc).flows![0].process![0];
    // Bare block lists, not wrapped flow objects.
    expect(block.process![0].type).toBe("rest");
    expect(block.error![0].type).toBe("set-payload");
    expect(block.settings).toBeUndefined();
  });

  it("round-trips handle-errors back into block-list slots", () => {
    const doc = emptyDocument();
    const he = newBlock("handle-errors");
    he.slots!.process[0].process = [newBlock("log")];
    he.slots!.error[0].process = [newBlock("set-payload")];
    doc.flows[0].process = [he];

    const restored = fromConfig(toConfig(doc));
    const node = restored.flows[0].process[0];
    expect(node.type).toBe("handle-errors");
    expect(node.slots!.process[0].process[0].type).toBe("log");
    expect(node.slots!.error[0].process[0].type).toBe("set-payload");
  });

  it("round-trips ai-retry: scalar AI fields plus block-list process/error", () => {
    const doc = emptyDocument();
    const retry = newBlock("ai-retry"); // seeds process/error block-list slots
    retry.settings.connector = "claude";
    retry.settings.prompt = "Fix the body from vars.error.";
    retry.settings.maxAttempts = 5;
    retry.slots!.process[0].process = [newBlock("rest")];
    retry.slots!.error[0].process = [newBlock("set-payload")];
    doc.flows[0].process = [retry];

    const block = toConfig(doc).flows![0].process![0];
    // Scalar AI fields hoist to top-level keys; process/error are bare block lists.
    expect(block.connector).toBe("claude");
    expect(block.prompt).toBe("Fix the body from vars.error.");
    expect(block.maxAttempts).toBe(5);
    expect(block.process![0].type).toBe("rest");
    expect(block.error![0].type).toBe("set-payload");
    expect(block.settings).toBeUndefined();

    const node = fromConfig(toConfig(doc)).flows[0].process[0];
    expect(node.type).toBe("ai-retry");
    expect(node.settings.connector).toBe("claude");
    expect(node.settings.maxAttempts).toBe(5);
    expect(node.slots!.process[0].process[0].type).toBe("rest");
    expect(node.slots!.error[0].process[0].type).toBe("set-payload");
  });

  it("serializes a flow-level error path as a bare block list", () => {
    const doc = emptyDocument();
    doc.flows[0].process = [newBlock("log")];
    doc.flows[0].error!.process = [newBlock("set-payload")];

    const flow = toConfig(doc).flows![0];
    expect(flow.process![0].type).toBe("log");
    expect(flow.error![0].type).toBe("set-payload"); // bare block list, sibling to process
  });

  it("round-trips a flow error path and seeds an empty one when absent", () => {
    const doc = emptyDocument();
    doc.flows[0].error!.process = [newBlock("log")];
    const restored = fromConfig(toConfig(doc));
    expect(restored.flows[0].error!.process[0].type).toBe("log");

    // A flow that declared no error path still gets an empty error chain.
    const seeded = fromConfig({
      flows: [{ name: "f", process: [{ type: "log" }] }],
    });
    expect(seeded.flows[0].error!.process).toEqual([]);
  });

  it("maps env declarations, dropping empty defaults and false required", () => {
    const doc = emptyDocument();
    doc.env = [
      { name: "WEATHER_LAT", default: "52.52" },
      { name: "API_KEY", required: true },
      { name: "EMPTY", default: "", required: false },
    ];
    expect(toConfig(doc).env).toEqual([
      { name: "WEATHER_LAT", default: "52.52" },
      { name: "API_KEY", required: true },
      { name: "EMPTY" },
    ]);
  });

  it("omits env entirely when none are declared", () => {
    expect(toConfig(emptyDocument()).env).toBeUndefined();
  });

  it("round-trips env declarations", () => {
    const doc = emptyDocument();
    doc.env = [{ name: "API_KEY", required: true }, { name: "LAT", default: "1" }];
    expect(fromConfig(toConfig(doc)).env).toEqual(doc.env);
  });
});
