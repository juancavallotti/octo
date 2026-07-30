import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Resource } from "@/app/model/orchestrator";

// The gates are the action's trust boundary and are exercised by the auth tests; here
// they are open so the storage behaviour is what's under test.
vi.mock("./_auth", () => ({
  withRead: (fn: (s: unknown) => unknown) => fn({}),
  withWrite: (fn: (s: unknown) => unknown) => fn({}),
}));

const client = {
  listResources: vi.fn(),
  createResource: vi.fn(),
  updateResource: vi.fn(),
  deleteResource: vi.fn(),
};
vi.mock("./_client", () => client);

const { deleteTestSuite, listTestSuites, saveTestSuite } = await import("./testSuites");

const res = (name: string, content: string): Resource => ({
  id: `id-${name}`,
  integrationId: "int-1",
  kind: "template",
  name,
  content,
  createdAt: "2026-01-01T00:00:00Z",
  lastUpdated: "2026-01-01T00:00:00Z",
});

/** What the orchestrator holds for this integration. */
function stored(...resources: Resource[]) {
  client.listResources.mockResolvedValue({ ok: true, data: resources });
}

const suite = (flow: string) => `flow: ${flow}\ncases: []\n`;

beforeEach(() => {
  vi.clearAllMocks();
  client.createResource.mockResolvedValue({ ok: true, data: res("x", "") });
  client.updateResource.mockResolvedValue({ ok: true, data: res("x", "") });
  client.deleteResource.mockResolvedValue({ ok: true, data: undefined });
  stored();
});

describe("listTestSuites", () => {
  it("returns the suites under `.octo/tests/`, keyed by the flow each declares", async () => {
    stored(
      res(".octo/tests/refunds_test.yaml", suite("refunds")),
      res(".octo/tests/order-intake_test.yaml", suite("Order Intake")),
    );

    expect(await listTestSuites("int-1")).toEqual({
      ok: true,
      data: [
        { flow: "Order Intake", content: suite("Order Intake") },
        { flow: "refunds", content: suite("refunds") },
      ],
    });
  });

  // The Resources tab, the Dev .env panel and the meta store share this namespace.
  // Claiming one of their files would let two tabs fight over it.
  it("ignores every resource that is not a suite", async () => {
    stored(
      res(".env.dev", "A=1"),
      res("templates/welcome.tmpl", "hi"),
      res(".octo/editor-meta.json", "{}"),
      res(".octo/tests/notes.txt", "x"),
      res(".octo/tests/orders_test.yaml", suite("orders")),
    );

    expect(await listTestSuites("int-1")).toEqual({
      ok: true,
      data: [{ flow: "orders", content: suite("orders") }],
    });
  });

  it("passes an orchestrator failure through", async () => {
    client.listResources.mockResolvedValue({ ok: false, error: "unreachable" });

    expect(await listTestSuites("int-1")).toEqual({ ok: false, error: "unreachable" });
  });
});

describe("saveTestSuite", () => {
  it("creates a slugified `_test.yaml` under `.octo/tests/`", async () => {
    expect(await saveTestSuite("int-1", "Order Intake", suite("Order Intake"))).toEqual({
      ok: true,
      data: undefined,
    });

    expect(client.createResource).toHaveBeenCalledWith(
      "int-1",
      "template",
      ".octo/tests/order-intake_test.yaml",
      suite("Order Intake"),
    );
    expect(client.updateResource).not.toHaveBeenCalled();
  });

  // Resolving by declaration rather than by name is what makes an edit land on the
  // suite the user is looking at, even when its name no longer matches the flow's slug.
  it("updates the resource already holding the flow's suite", async () => {
    stored(res(".octo/tests/renamed_test.yaml", suite("orders")));

    await saveTestSuite("int-1", "orders", `${suite("orders")}# edited\n`);

    expect(client.updateResource).toHaveBeenCalledWith(
      "int-1",
      "id-.octo/tests/renamed_test.yaml",
      "template",
      ".octo/tests/renamed_test.yaml",
      `${suite("orders")}# edited\n`,
    );
    expect(client.createResource).not.toHaveBeenCalled();
  });

  // Two distinct flow names can slug to one stem. Letting the second overwrite the
  // first would delete a test the user wrote, silently.
  it("de-duplicates a name already taken by a different flow", async () => {
    stored(res(".octo/tests/order-intake_test.yaml", suite("Order Intake")));

    await saveTestSuite("int-1", "order-intake", suite("order-intake"));

    expect(client.createResource).toHaveBeenCalledWith(
      "int-1",
      "template",
      ".octo/tests/order-intake-2_test.yaml",
      suite("order-intake"),
    );
  });

  it("refuses a suite that names no flow", async () => {
    expect(await saveTestSuite("int-1", "  ", suite("x"))).toEqual({
      ok: false,
      error: "a suite must name the flow it tests",
    });
    expect(client.createResource).not.toHaveBeenCalled();
  });

  it("passes a write failure through", async () => {
    client.createResource.mockResolvedValue({ ok: false, error: "forbidden" });

    expect(await saveTestSuite("int-1", "orders", suite("orders"))).toEqual({
      ok: false,
      error: "forbidden",
    });
  });
});

describe("deleteTestSuite", () => {
  // There is no delete-by-name on the orchestrator, so the resource is resolved first.
  it("deletes the resource holding the flow's suite, by id", async () => {
    stored(
      res(".octo/tests/orders_test.yaml", suite("orders")),
      res(".octo/tests/refunds_test.yaml", suite("refunds")),
    );

    expect(await deleteTestSuite("int-1", "orders")).toEqual({ ok: true, data: undefined });
    expect(client.deleteResource).toHaveBeenCalledWith(
      "int-1",
      "id-.octo/tests/orders_test.yaml",
    );
  });

  it("is a no-op for a flow that has no suite", async () => {
    stored(res(".octo/tests/refunds_test.yaml", suite("refunds")));

    expect(await deleteTestSuite("int-1", "orders")).toEqual({ ok: true, data: undefined });
    expect(client.deleteResource).not.toHaveBeenCalled();
  });

  it("refuses a delete that names no flow", async () => {
    expect(await deleteTestSuite("int-1", "")).toEqual({
      ok: false,
      error: "a suite must name the flow it tests",
    });
    expect(client.deleteResource).not.toHaveBeenCalled();
  });
});
