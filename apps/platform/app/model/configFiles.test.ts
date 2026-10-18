import { beforeEach, describe, expect, it, vi } from "vitest";

const integrations = vi.hoisted(() => ({
  readConfigFiles: vi.fn(),
  replaceConfigFiles: vi.fn(),
  createIntegration: vi.fn(),
  deleteIntegration: vi.fn(),
}));
const resources = vi.hoisted(() => ({
  listResources: vi.fn(),
  createResource: vi.fn(),
  upsertResource: vi.fn(),
  deleteResource: vi.fn(),
}));
vi.mock("@/app/actions/integrations", () => integrations);
vi.mock("@/app/actions/resources", () => resources);

import { duplicateIntegration } from "./configFiles";

const ok = <T,>(data: T) => ({ ok: true as const, data });

function resource(name: string, kind = "template") {
  return { id: `r-${name}`, integrationId: "src", kind, name, content: `content of ${name}` };
}

beforeEach(() => {
  vi.clearAllMocks();
  integrations.readConfigFiles.mockResolvedValue(
    ok([
      { path: "octo.yaml", content: "service:\n  name: shop\n" },
      { path: "orders.yaml", content: "flows: []\n" },
    ]),
  );
  integrations.createIntegration.mockResolvedValue(ok({ id: "copy", name: "Copy of shop" }));
  integrations.replaceConfigFiles.mockResolvedValue(ok(undefined));
  integrations.deleteIntegration.mockResolvedValue(ok(undefined));
  resources.listResources.mockResolvedValue(
    ok([
      resource("templates/welcome.tmpl"),
      resource(".env.dev", "env"),
      resource(".octo/tests/orders_test.yaml"),
      resource(".octo/editor-meta.json"),
    ]),
  );
  resources.createResource.mockResolvedValue(ok(resource("x")));
});

describe("duplicateIntegration", () => {
  it("copies every config file and every resource, suites included", async () => {
    const created = await duplicateIntegration("src", "Copy of shop");

    expect(created.id).toBe("copy");
    expect(integrations.replaceConfigFiles).toHaveBeenCalledWith("copy", [
      { path: "octo.yaml", content: "service:\n  name: shop\n" },
      { path: "orders.yaml", content: "flows: []\n" },
    ]);
    const copied = resources.createResource.mock.calls.map(([id, kind, name, content]) => ({
      id,
      kind,
      name,
      content,
    }));
    expect(copied).toEqual([
      { id: "copy", kind: "template", name: "templates/welcome.tmpl", content: "content of templates/welcome.tmpl" },
      { id: "copy", kind: "env", name: ".env.dev", content: "content of .env.dev" },
      {
        id: "copy",
        kind: "template",
        name: ".octo/tests/orders_test.yaml",
        content: "content of .octo/tests/orders_test.yaml",
      },
    ]);
  });

  // Its entries are keyed by the source's id, which the copy's editor never reads.
  it("leaves the editor's metadata behind", async () => {
    await duplicateIntegration("src", "Copy of shop");
    const names = resources.createResource.mock.calls.map((c) => c[2]);
    expect(names).not.toContain(".octo/editor-meta.json");
  });

  it("deletes the copy when a resource cannot be copied", async () => {
    resources.createResource
      .mockResolvedValueOnce(ok(resource("templates/welcome.tmpl")))
      .mockResolvedValueOnce({ ok: false, error: "conflict", status: 409 });

    await expect(duplicateIntegration("src", "Copy of shop")).rejects.toThrow();
    expect(integrations.deleteIntegration).toHaveBeenCalledWith("copy");
  });
});
