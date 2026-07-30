import { describe, expect, it } from "vitest";
import { flowOfSuite, isSuiteFileName, suiteFileName } from "./naming";

describe("suiteFileName", () => {
  it("slugifies a flow name into a `_test.yaml` filename", () => {
    expect(suiteFileName("orders")).toBe("orders_test.yaml");
    expect(suiteFileName("Order Intake")).toBe("order-intake_test.yaml");
    expect(suiteFileName("  Checkout / Pipeline  ")).toBe("checkout-pipeline_test.yaml");
  });

  // A flow name is free text and a filename is not. Collapsing every non-alphanumeric
  // means no name can produce a separator, a leading dot, or `..`.
  it("cannot mint a name that escapes a directory", () => {
    expect(suiteFileName("../../etc/passwd")).toBe("etc-passwd_test.yaml");
    expect(suiteFileName(".hidden")).toBe("hidden_test.yaml");
    expect(suiteFileName("a/b\\c")).toBe("a-b-c_test.yaml");
  });

  it("falls back to a usable name when the flow name slugs to nothing", () => {
    expect(suiteFileName("")).toBe("flow_test.yaml");
    expect(suiteFileName("!!!")).toBe("flow_test.yaml");
  });

  it("mints a name it recognises as a suite", () => {
    for (const flow of ["orders", "Order Intake", "!!!", "../etc"]) {
      expect(isSuiteFileName(suiteFileName(flow)), flow).toBe(true);
    }
  });
});

describe("isSuiteFileName", () => {
  it("accepts suites and rejects flows and other files", () => {
    expect(isSuiteFileName("orders_test.yaml")).toBe(true);
    expect(isSuiteFileName("orders_test.yml")).toBe(true);
    expect(isSuiteFileName("Orders_TEST.YAML")).toBe(true);
    expect(isSuiteFileName("orders.yaml")).toBe(false);
    expect(isSuiteFileName("orders_test.json")).toBe(false);
    expect(isSuiteFileName("test.yaml")).toBe(false);
    expect(isSuiteFileName(".orders_test.yaml")).toBe(false);
  });

  // The platform stores suites under `.octo/tests/`, so the check has to look at the
  // last segment — but only at the last segment, or a directory could smuggle one in.
  it("reads the last segment of a path-like resource name", () => {
    expect(isSuiteFileName(".octo/tests/orders_test.yaml")).toBe(true);
    expect(isSuiteFileName(".octo/tests/orders.yaml")).toBe(false);
    expect(isSuiteFileName("orders_test.yaml/nested.txt")).toBe(false);
  });
});

describe("flowOfSuite", () => {
  it("reads the flow the suite declares", () => {
    expect(flowOfSuite("flow: orders\ncases: []\n", "orders_test.yaml")).toBe("orders");
  });

  // The name a filename cannot spell is exactly the case this exists for.
  it("recovers a flow name that its filename cannot spell", () => {
    const file = suiteFileName("Order Intake");
    expect(flowOfSuite("flow: Order Intake\n", file)).toBe("Order Intake");
  });

  it("reads quoted forms and ignores a trailing comment", () => {
    expect(flowOfSuite(`flow: "My Flow"  # the one\n`, "x_test.yaml")).toBe("My Flow");
    expect(flowOfSuite(`flow: 'My Flow'\n`, "x_test.yaml")).toBe("My Flow");
    expect(flowOfSuite(`flow: bare  # trailing\n`, "x_test.yaml")).toBe("bare");
  });

  // `flow:` is a top-level key, so only column zero counts. A nested key of the same
  // name — or one inside a block scalar — is not the declaration.
  it("does not mistake a nested `flow:` for the declaration", () => {
    const content = [
      "cases:",
      "  - name: one",
      "    expect:",
      "      body:",
      "        flow: nested",
      "",
    ].join("\n");
    expect(flowOfSuite(content, "orders_test.yaml")).toBe("orders");
  });

  it("falls back to the filename stem when nothing is declared yet", () => {
    expect(flowOfSuite("# half written\n", "orders_test.yaml")).toBe("orders");
    expect(flowOfSuite("", "refunds_test.yml")).toBe("refunds");
    expect(flowOfSuite("flow:\n", "orders_test.yaml")).toBe("orders");
    expect(flowOfSuite("", ".octo/tests/orders_test.yaml")).toBe("orders");
  });
});
