import { describe, it, expect } from "vitest";
import { Box } from "lucide-react";
import {
  CAPABILITIES,
  getBlockSpec,
  getConnectorSpec,
  listBlocks,
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
