import { describe, it, expect } from "vitest";
import YAML from "yaml";
import { emptyFlow, newBlock, type EditorDocument } from "./document";
import {
  RUN_SERVICE_NAME,
  fromDefinitionYaml,
  toDefinitionYaml,
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

describe("integration definition YAML", () => {
  it("stamps the integration name as the service name", () => {
    const parsed = YAML.parse(toDefinitionYaml(doc(), "Order Sync"));
    expect(parsed.service.name).toBe("Order Sync");
  });

  it("falls back to the run service name when unnamed", () => {
    const parsed = YAML.parse(toDefinitionYaml(doc(), "  "));
    expect(parsed.service.name).toBe(RUN_SERVICE_NAME);
  });

  it("round-trips a document through definition YAML", () => {
    const restored = fromDefinitionYaml(toDefinitionYaml(doc(), "Order Sync"));
    expect(restored.flows).toHaveLength(1);
    expect(restored.flows[0].name).toBe("ticker");
    expect(restored.flows[0].source).toMatchObject({
      connector: "cron",
      type: "cron",
      settings: { schedule: "@every 2s" },
    });
    expect(restored.flows[0].process[0].type).toBe("log");
  });

  it("parses an empty definition into an empty document", () => {
    expect(fromDefinitionYaml("").flows).toHaveLength(1);
  });
});
