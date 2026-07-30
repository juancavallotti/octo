import { afterAll, beforeEach, describe, expect, it } from "vitest";
import { mkdtemp, rm, readdir, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import {
  createFlow,
  listFlows,
  readFlow,
  updateFlow,
  writeFlow,
} from "./store";

// A real temp directory backs the store (OCTO_FS_DIR); each test starts clean.
let dir: string;

beforeEach(async () => {
  dir = await mkdtemp(path.join(tmpdir(), "octo-fs-"));
  process.env.OCTO_FS_DIR = dir;
});

afterAll(async () => {
  if (dir) await rm(dir, { recursive: true, force: true });
});

describe("standalone flow store", () => {
  it("creates a slugified `.yaml` file from the name", async () => {
    const doc = await createFlow("My First Flow", "name: x\n");
    expect(doc.id).toBe("my-first-flow.yaml");
    expect(doc.name).toBe("my-first-flow");
    expect(await readdir(dir)).toEqual(["my-first-flow.yaml"]);
  });

  it("renames the file on disk when the name's slug changes", async () => {
    await createFlow("My First Flow", "name: x\n");
    const renamed = await updateFlow(
      "my-first-flow.yaml",
      "Checkout Pipeline",
      "name: y\n",
    );
    expect(renamed.id).toBe("checkout-pipeline.yaml");
    // old file gone, new file present, content updated
    expect(await readdir(dir)).toEqual(["checkout-pipeline.yaml"]);
    expect((await readFlow("checkout-pipeline.yaml")).definition).toBe(
      "name: y\n",
    );
  });

  it("updates in place when the name's slug is unchanged", async () => {
    await createFlow("Checkout", "name: x\n");
    const updated = await updateFlow(
      "checkout.yaml",
      "checkout",
      "name: edited\n",
    );
    expect(updated.id).toBe("checkout.yaml");
    expect(await readdir(dir)).toEqual(["checkout.yaml"]);
    expect((await readFlow("checkout.yaml")).definition).toBe("name: edited\n");
  });

  it("de-duplicates a rename that would collide with another flow", async () => {
    await createFlow("Orders", "name: a\n");
    await createFlow("Shipments", "name: b\n"); // -> shipments.yaml
    const renamed = await updateFlow("orders.yaml", "Shipments", "name: a\n");
    expect(renamed.id).toBe("shipments-2.yaml");
    expect((await readdir(dir)).sort()).toEqual([
      "shipments-2.yaml",
      "shipments.yaml",
    ]);
  });

  it("rejects ids that try to escape the store root", async () => {
    const attacks = [
      "../secret.yaml", // parent dir
      "../../etc/passwd.yaml", // deep traversal
      "/etc/passwd.yaml", // absolute path
      "sub/dir.yaml", // nested dir
      "a/../../b.yaml", // separators + traversal
      "..\\win.yaml", // backslash separator
      ".hidden.yaml", // leading-dot dotfile
      "x.txt", // not a yaml
      "noext", // no extension
    ];
    for (const id of attacks) {
      await expect(readFlow(id), id).rejects.toThrow();
      await expect(writeFlow(id, "x"), id).rejects.toThrow();
      await expect(updateFlow(id, "ok", "x"), id).rejects.toThrow();
    }
    // Nothing got written outside (or inside) the root.
    expect(await readdir(dir)).toEqual([]);
  });

  it("sanitizes traversal attempts in a creation/rename name to a safe filename", async () => {
    // The name is slugified, so separators and dots can't escape the root.
    const created = await createFlow("../../etc/passwd", "x");
    expect(created.id).toBe("etc-passwd.yaml");
    await createFlow("Orders", "y"); // -> orders.yaml
    const renamed = await updateFlow("orders.yaml", "../../root/.ssh/id", "z");
    expect(renamed.id).toBe("root-ssh-id.yaml");
    // Every file landed directly in the root.
    for (const f of await readdir(dir)) expect(f).not.toContain(path.sep);
  });

  it("lists flows by id and derived name, sorted", async () => {
    await writeFile(path.join(dir, "b.yaml"), "");
    await writeFile(path.join(dir, "a.yaml"), "");
    await writeFile(path.join(dir, "ignore.txt"), "");
    expect(await listFlows()).toEqual([
      { id: "a.yaml", name: "a" },
      { id: "b.yaml", name: "b" },
    ]);
  });

  // A dolphin suite lives beside the flow it tests, exactly as octo's own directory
  // loader expects (runtime/core/runtime/config.go skips these). Listing one would
  // offer it in the folder picker as an integration of its own.
  it("does not list test suites as flows", async () => {
    await writeFile(path.join(dir, "orders.yaml"), "");
    await writeFile(path.join(dir, "orders_test.yaml"), "");
    await writeFile(path.join(dir, "refunds_test.yml"), "");

    expect(await listFlows()).toEqual([{ id: "orders.yaml", name: "orders" }]);
  });

  // Excluding suites from the listing alone would leave the split a convention. The
  // damaging direction is a write: it would replace a suite with a flow definition.
  it("refuses to read or write a test suite as a flow", async () => {
    await writeFile(path.join(dir, "orders_test.yaml"), "flow: orders\n");

    await expect(readFlow("orders_test.yaml")).rejects.toThrow(/test suites/);
    await expect(writeFlow("orders_test.yaml", "clobbered")).rejects.toThrow(/test suites/);
    // The suite is untouched.
    expect(await readFile(path.join(dir, "orders_test.yaml"), "utf8")).toBe("flow: orders\n");
  });

  // slugify maps every non-alphanumeric to `-`, so the editor's own save path cannot
  // mint a name that collides with the suite namespace.
  it("cannot create a flow whose name would look like a test suite", async () => {
    const created = await createFlow("orders_test", "x");
    expect(created.id).toBe("orders-test.yaml");
  });
});
