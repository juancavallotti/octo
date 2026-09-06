import { describe, expect, it } from "vitest";
import { byFamily, familyOf } from "./family";

describe("familyOf", () => {
  it("sorts the three collectors apart", () => {
    expect(familyOf("octo_flows").key).toBe("octo");
    expect(familyOf("process_open_fds").key).toBe("process");
    expect(familyOf("go_goroutines").key).toBe("go");
  });

  it("has somewhere to put what it does not recognize", () => {
    // promhttp_metric_handler_errors_total is real and belongs to none of them.
    expect(familyOf("promhttp_metric_handler_errors_total").key).toBe("other");
  });
});

describe("byFamily", () => {
  it("orders sections by where the answer usually is", () => {
    const groups = byFamily([
      { name: "go_goroutines" },
      { name: "process_open_fds" },
      { name: "octo_flows" },
    ]);
    expect(groups.map((g) => g.family.key)).toEqual(["octo", "process", "go"]);
  });

  it("omits a family the runtime exposes nothing under", () => {
    expect(byFamily([{ name: "go_threads" }]).map((g) => g.family.key)).toEqual(["go"]);
  });

  it("puts histogram buckets last within their family", () => {
    // One of them is more series than every other metric combined, and its chart
    // says the least at a glance.
    const items = byFamily([
      { name: "octo_flow_duration_seconds_bucket" },
      { name: "octo_ready" },
      { name: "octo_flows" },
    ])[0].items;
    expect(items.map((i) => i.name)).toEqual([
      "octo_flows",
      "octo_ready",
      "octo_flow_duration_seconds_bucket",
    ]);
  });
});
