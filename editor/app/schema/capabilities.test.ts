import { describe, it, expect } from "vitest";
import { Box } from "lucide-react";
import {
  CAPABILITIES,
  getBlockSpec,
  getConnectorSpec,
  getSourceSpec,
  listBlocks,
  listSources,
  resolveIcon,
} from "./index";

describe("capability schema", () => {
  it("exposes blocks and connectors", () => {
    expect(CAPABILITIES.blocks.length).toBeGreaterThan(0);
    expect(CAPABILITIES.connectors.length).toBeGreaterThan(0);
  });

  it("gives every block a label, category and at least one field", () => {
    for (const block of listBlocks()) {
      expect(block.label, block.type).toBeTruthy();
      expect(["processor", "control-flow"]).toContain(block.category);
      expect(block.fields.length, block.type).toBeGreaterThan(0);
    }
  });

  it("resolves every block icon to a real component (no fallbacks)", () => {
    for (const block of listBlocks()) {
      expect(resolveIcon(block.icon), block.icon).not.toBe(Box);
    }
  });

  it("falls back to a generic icon for unknown names", () => {
    expect(resolveIcon("does-not-exist")).toBe(Box);
  });

  it("looks blocks and connectors up by type", () => {
    expect(getBlockSpec("log")?.label).toBe("Log");
    expect(getBlockSpec("nope")).toBeUndefined();
    expect(getConnectorSpec("http")?.label).toBe("HTTP Server");
    expect(getConnectorSpec("nope")).toBeUndefined();
  });

  it("lists every connector source paired with its connector", () => {
    const sources = listSources();
    expect(sources.length).toBeGreaterThan(0);
    const cron = sources.find((s) => s.spec.type === "cron");
    expect(cron).toMatchObject({ connector: "cron", connectorLabel: "Cron" });
    expect(cron?.spec.label).toBe("Cron schedule");
    // Every listed source maps back to a real connector.
    for (const s of sources) {
      expect(getConnectorSpec(s.connector), s.connector).toBeDefined();
    }
  });

  it("resolves every source icon to a real component (no fallbacks)", () => {
    for (const { spec } of listSources()) {
      expect(spec.icon, spec.type).toBeTruthy();
      expect(resolveIcon(spec.icon ?? ""), spec.icon).not.toBe(Box);
    }
  });

  it("looks a source up by its connector and type", () => {
    expect(getSourceSpec("http", "http")?.label).toBe("HTTP route");
    expect(getSourceSpec("cron", "nope")).toBeUndefined();
    expect(getSourceSpec("nope", "http")).toBeUndefined();
  });

  it("gives required enum fields a non-empty option list", () => {
    for (const block of listBlocks()) {
      for (const field of block.fields) {
        if (field.type === "enum") {
          expect(field.enum?.length, `${block.type}.${field.name}`).toBeGreaterThan(0);
        }
      }
    }
  });
});
