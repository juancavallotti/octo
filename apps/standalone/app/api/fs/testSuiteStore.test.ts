import { afterAll, beforeEach, describe, expect, it } from "vitest";
import { mkdtemp, readdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { deleteSuite, listSuites, writeSuite } from "./testSuiteStore";
import { createFlow, listFlows } from "./store";

// A real temp directory backs the store (OCTO_FS_DIR); each test starts clean.
let dir: string;

beforeEach(async () => {
  dir = await mkdtemp(path.join(tmpdir(), "octo-suite-"));
  process.env.OCTO_FS_DIR = dir;
});

afterAll(async () => {
  if (dir) await rm(dir, { recursive: true, force: true });
});

const suite = (flow: string) => `flow: ${flow}\ncases:\n  - name: ok\n`;

describe("standalone test suite store", () => {
  it("writes a slugified `_test.yaml` beside the flows", async () => {
    await writeSuite("orders", suite("orders"));

    expect(await readdir(dir)).toEqual(["orders_test.yaml"]);
    expect(await readFile(path.join(dir, "orders_test.yaml"), "utf8")).toBe(suite("orders"));
  });

  it("lists suites by the flow each one declares", async () => {
    await writeSuite("refunds", suite("refunds"));
    await writeSuite("orders", suite("orders"));

    expect(await listSuites()).toEqual([
      { flow: "orders", content: suite("orders") },
      { flow: "refunds", content: suite("refunds") },
    ]);
  });

  // slugify is not reversible, so deriving the flow from the filename would hand the
  // editor a name no flow in the document has — and the tab would show the suite as
  // missing while the file sat right there.
  it("recovers a flow name that its filename cannot spell", async () => {
    await writeSuite("Order Intake", suite("Order Intake"));

    expect(await readdir(dir)).toEqual(["order-intake_test.yaml"]);
    expect(await listSuites()).toEqual([
      { flow: "Order Intake", content: suite("Order Intake") },
    ]);
  });

  it("reads a quoted flow declaration, and ignores a trailing comment", async () => {
    await writeFile(path.join(dir, "a_test.yaml"), `flow: "My Flow"  # the one\n`);
    await writeFile(path.join(dir, "b_test.yaml"), `flow: 'Other'\n`);

    expect((await listSuites()).map((s) => s.flow)).toEqual(["My Flow", "Other"]);
  });

  // `flow:` is a top-level key, so only column zero counts. A nested key of the same
  // name — or one inside a block scalar — must not be mistaken for the declaration.
  it("does not mistake a nested `flow:` for the declaration", async () => {
    await writeFile(
      path.join(dir, "orders_test.yaml"),
      ["cases:", "  - name: one", "    expect:", "      body:", "        flow: nested", ""].join(
        "\n",
      ),
    );

    // No top-level declaration, so the filename stem is the fallback.
    expect((await listSuites()).map((s) => s.flow)).toEqual(["orders"]);
  });

  it("falls back to the filename for a suite that declares no flow yet", async () => {
    await writeFile(path.join(dir, "orders_test.yaml"), "# half written\n");
    await writeFile(path.join(dir, "refunds_test.yml"), "");

    expect((await listSuites()).map((s) => s.flow)).toEqual(["orders", "refunds"]);
  });

  // The file, not the name, is the identity: an edit must land where the suite already
  // is, so a hand-renamed file keeps being the one we update.
  it("rewrites the file already holding the flow's suite", async () => {
    await writeFile(path.join(dir, "renamed-by-hand_test.yaml"), suite("orders"));

    await writeSuite("orders", `${suite("orders")}# edited\n`);

    expect(await readdir(dir)).toEqual(["renamed-by-hand_test.yaml"]);
    expect(await readFile(path.join(dir, "renamed-by-hand_test.yaml"), "utf8")).toContain(
      "# edited",
    );
  });

  // Two distinct flow names can slug to one stem. Letting the second overwrite the first
  // would delete a test the user wrote, silently.
  it("de-duplicates two flows whose names slug the same", async () => {
    await writeSuite("Order Intake", suite("Order Intake"));
    await writeSuite("order-intake", suite("order-intake"));

    expect((await readdir(dir)).sort()).toEqual([
      "order-intake-2_test.yaml",
      "order-intake_test.yaml",
    ]);
    expect((await listSuites()).map((s) => s.flow).sort()).toEqual([
      "Order Intake",
      "order-intake",
    ]);
  });

  it("deletes the suite for a flow, and ignores a flow that has none", async () => {
    await writeSuite("orders", suite("orders"));
    await writeSuite("refunds", suite("refunds"));

    await deleteSuite("orders");
    await deleteSuite("never-had-one");

    expect(await readdir(dir)).toEqual(["refunds_test.yaml"]);
  });

  it("ignores everything in the root that is not a suite", async () => {
    await writeFile(path.join(dir, "orders.yaml"), "name: orders\n");
    await writeFile(path.join(dir, "notes.txt"), "");
    await writeFile(path.join(dir, ".env.dev"), "");
    await writeFile(path.join(dir, "orders_test.yaml"), suite("orders"));

    expect((await listSuites()).map((s) => s.flow)).toEqual(["orders"]);
  });

  it("returns nothing when the flows directory does not exist yet", async () => {
    process.env.OCTO_FS_DIR = path.join(dir, "not-created");

    expect(await listSuites()).toEqual([]);
  });

  // A flow name is user input. slugify strips every separator, so no name can name a
  // file outside the root — and the resolved-parent check backs that up.
  it("sanitizes a flow name that tries to escape the root", async () => {
    await writeSuite("../../etc/passwd", suite("../../etc/passwd"));

    expect(await readdir(dir)).toEqual(["etc-passwd_test.yaml"]);
    for (const f of await readdir(dir)) expect(f).not.toContain(path.sep);
  });

  // The two stores partition the flows root, and this is the seam: what one writes, the
  // other must not claim. A suite written here stays out of the flow list, and a flow
  // created there is never read back as a suite.
  it("keeps a disjoint partition with the flow store", async () => {
    await createFlow("Orders", "name: orders\n");
    await writeSuite("orders", suite("orders"));

    expect(await listFlows()).toEqual([{ id: "orders.yaml", name: "orders" }]);
    expect((await listSuites()).map((s) => s.flow)).toEqual(["orders"]);
  });
});
