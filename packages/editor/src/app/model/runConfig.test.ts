import { describe, it, expect } from "vitest";
import YAML from "yaml";
import { emptyFlow, newBlock, type EditorDocument } from "./document";
import {
  RUN_SERVICE_NAME,
  fromDefinitionYaml,
  toFileYaml,
  toRunnableYaml,
} from "./runConfig";

function doc(): EditorDocument {
  const flow = emptyFlow("ticker");
  flow.source = {
    connector: "cron",
    type: "cron",
    settings: { schedule: "@every 2s" },
  };
  flow.process = [newBlock("log")];
  return { flows: [flow], connectors: [], processors: [], env: [] };
}

describe("toRunnableYaml", () => {
  it("emits parseable YAML with a service block and the flows", () => {
    const yaml = toRunnableYaml(doc());
    expect(yaml).toContain("service:");

    const parsed = YAML.parse(yaml);
    expect(parsed.service.name).toBe(RUN_SERVICE_NAME);
    expect(parsed.flows).toHaveLength(1);
    expect(parsed.flows[0].name).toBe("ticker");
    expect(parsed.flows[0].source).toMatchObject({
      connector: "cron",
      type: "cron",
      settings: { schedule: "@every 2s" },
    });
    expect(parsed.flows[0].process[0].type).toBe("log");
  });
});

describe("file YAML", () => {
  it("stamps the service name when given one", () => {
    const parsed = YAML.parse(toFileYaml(doc(), { service: "Order Sync" }));
    expect(parsed.service.name).toBe("Order Sync");
  });

  it("falls back to the run service name when the given name is blank", () => {
    const parsed = YAML.parse(toFileYaml(doc(), { service: "  " }));
    expect(parsed.service.name).toBe(RUN_SERVICE_NAME);
  });

  it("writes no service when not given one", () => {
    expect(YAML.parse(toFileYaml(doc())).service).toBeUndefined();
  });

  it("round-trips a document", () => {
    const restored = fromDefinitionYaml(toFileYaml(doc(), { service: "Order Sync" }));
    expect(restored.flows).toHaveLength(1);
    expect(restored.flows[0].name).toBe("ticker");
    expect(restored.flows[0].source).toMatchObject({
      connector: "cron",
      type: "cron",
      settings: { schedule: "@every 2s" },
    });
    expect(restored.flows[0].process[0].type).toBe("log");
  });

  it("parses an empty definition into a document with no flows", () => {
    expect(fromDefinitionYaml("").flows).toHaveLength(0);
  });
});
