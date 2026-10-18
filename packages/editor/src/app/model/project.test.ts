import { describe, it, expect } from "vitest";
import YAML from "yaml";
import {
  parseProject,
  projectView,
  sameFiles,
  serializeProject,
  serviceFile,
  isConfigFileName,
  mergeConfigFiles,
} from "./project";

const octoYaml = `
service:
  name: shop
env:
  - name: REGION
    default: eu
resources:
  templates:
    - resource: templates/receipt.tmpl
      as: receipt
connectors:
  - name: orders-api
    type: http
`;

const ordersYaml = `
env:
  - name: REGION
    default: us
  - name: API_KEY
flows:
  - name: orders
    source:
      connector: orders-api
      type: route
      settings:
        path: /orders
    process:
      - type: flow-ref
        settings:
          flow: audit
`;

const auditYaml = `
flows:
  - name: audit
    process: []
`;

const stored = [
  { path: "orders.yaml", content: ordersYaml },
  { path: "octo.yaml", content: octoYaml },
  { path: "audit.yaml", content: auditYaml },
];

describe("parseProject", () => {
  it("orders files by path and records which declares the service", () => {
    const files = parseProject(stored);
    expect(files.map((f) => [f.path, f.declaresService])).toEqual([
      ["audit.yaml", false],
      ["octo.yaml", true],
      ["orders.yaml", false],
    ]);
  });

  it("binds a source to a connection another file declares", () => {
    const orders = parseProject(stored).find((f) => f.path === "orders.yaml")!;
    expect(orders.document.flows[0].source).toMatchObject({
      connector: "http",
      connectorRef: "orders-api",
    });
  });

  it("leaves a file of connections alone without flows", () => {
    const octo = parseProject(stored).find((f) => f.path === "octo.yaml")!;
    expect(octo.document.flows).toEqual([]);
    expect(octo.document.connectors.map((c) => c.name)).toEqual(["orders-api"]);
  });
});

describe("projectView", () => {
  it("merges the files the way the runtime does", () => {
    const view = projectView(parseProject(stored));
    expect(view.flows.map((f) => f.name)).toEqual(["audit", "orders"]);
    expect(view.connectors.map((c) => c.name)).toEqual(["orders-api"]);
    // The first declaration by file name wins: octo.yaml sorts before orders.yaml.
    expect(view.env).toEqual([
      { name: "REGION", default: "eu" },
      { name: "API_KEY" },
    ]);
    expect(view.resources?.templates).toEqual([{ resource: "templates/receipt.tmpl", as: "receipt" }]);
  });
});

describe("serializeProject", () => {
  it("writes the service into the file that declares it and no other", () => {
    const out = serializeProject(parseProject(stored), "Shop");
    const services = out.map((f) => [f.path, YAML.parse(f.content).service?.name]);
    expect(services).toEqual([
      ["audit.yaml", undefined],
      ["octo.yaml", "Shop"],
      ["orders.yaml", undefined],
    ]);
  });

  it("keeps a cross-file binding by name and writes no flows key for a file of connections", () => {
    const out = serializeProject(parseProject(stored), "Shop");
    const orders = YAML.parse(out.find((f) => f.path === "orders.yaml")!.content);
    expect(orders.flows[0].source.connector).toBe("orders-api");
    const octo = YAML.parse(out.find((f) => f.path === "octo.yaml")!.content);
    expect(octo.flows).toBeUndefined();
  });

  it("puts the service in the first file when none declares it", () => {
    const files = parseProject([
      { path: "b.yaml", content: auditYaml },
      { path: "a.yaml", content: "flows: []\n" },
    ]);
    expect(serviceFile(files)).toBe("a.yaml");
  });

  it("writes a file nobody touched back as it was loaded, comments and all", () => {
    const files = parseProject([{ path: "orders.yaml", content: "# keep me\n" + ordersYaml }, ...stored.slice(1)]);
    const out = serializeProject(files, "shop");
    expect(out.find((f) => f.path === "orders.yaml")!.content).toBe("# keep me\n" + ordersYaml);
    expect(out.find((f) => f.path === "octo.yaml")!.content).toBe(octoYaml);
  });

  it("re-serializes an edited file, and the service file when the project is renamed", () => {
    const files = parseProject(stored).map((f) =>
      f.path === "audit.yaml" ? { ...f, document: { ...f.document } } : f,
    );
    const out = serializeProject(files, "Shop");
    expect(out.find((f) => f.path === "audit.yaml")!.content).not.toBe(auditYaml);
    expect(YAML.parse(out.find((f) => f.path === "octo.yaml")!.content).service.name).toBe("Shop");
    expect(out.find((f) => f.path === "orders.yaml")!.content).toBe(ordersYaml);
  });

  it("round-trips", () => {
    const files = parseProject(stored);
    const again = parseProject(serializeProject(files, "Shop"));
    expect(projectView(again).flows.map((f) => f.name)).toEqual(["audit", "orders"]);
    expect(again.find((f) => f.path === "octo.yaml")?.declaresService).toBe(true);
  });
});

describe("sameFiles", () => {
  it("is identity per file, not equality", () => {
    const files = parseProject(stored);
    expect(sameFiles(files, [...files].reverse())).toBe(true);
    expect(sameFiles(files, parseProject(stored))).toBe(false);
    expect(sameFiles(files, files.slice(1))).toBe(false);
  });
});

describe("isConfigFileName", () => {
  it.each([
    ["orders.yaml", true],
    ["orders.yml", true],
    ["orders_test.yaml", false],
    ["nested/orders.yaml", false],
    ["notes.md", false],
  ])("%s → %s", (path, want) => {
    expect(isConfigFileName(path)).toBe(want);
  });
});

describe("mergeConfigFiles", () => {
  it("merges by the runtime's rules", () => {
    const merged = YAML.parse(mergeConfigFiles(stored));
    expect(merged.service).toEqual({ name: "shop" });
    expect(merged.flows.map((f: { name: string }) => f.name)).toEqual(["audit", "orders"]);
    expect(merged.connectors.map((c: { name: string }) => c.name)).toEqual(["orders-api"]);
    expect(merged.env).toEqual([{ name: "REGION", default: "eu" }, { name: "API_KEY" }]);
    expect(merged.resources.templates).toEqual([{ resource: "templates/receipt.tmpl", as: "receipt" }]);
  });

  it("keeps duplicates for the runtime to name", () => {
    const merged = YAML.parse(
      mergeConfigFiles([
        { path: "a.yaml", content: auditYaml },
        { path: "b.yaml", content: auditYaml },
      ]),
    );
    expect(merged.flows).toHaveLength(2);
  });

  it("returns a single file verbatim", () => {
    expect(mergeConfigFiles([{ path: "octo.yaml", content: "# kept\n" + auditYaml }])).toBe("# kept\n" + auditYaml);
  });
});
