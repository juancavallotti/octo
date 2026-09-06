import { describe, expect, it } from "vitest";
import { unitFor } from "./units";

/**
 * Prometheus carries no unit metadata, so the suffix convention in the name is
 * all there is. Every case below is one this deployment's own catalogue
 * produced — these are not hypotheticals.
 */
describe("unitFor", () => {
  it("reads bytes, and a byte counter as a rate", () => {
    expect(unitFor("process_resident_memory_bytes", "gauge").format(133_000_000))
      .toBe("127 MiB");
    expect(unitFor("process_network_receive_bytes_total", "counter").format(5000))
      .toBe("5 KiB/s");
  });

  it("says unlimited rather than eight exabytes", () => {
    // Go writes math.MaxInt64 for "no memory limit" and the kernel does the same
    // for an unset rlimit. "8388608 TiB" is a worse way of saying so.
    expect(unitFor("go_gc_gomemlimit_bytes", "gauge").format(9.223372036854776e18))
      .toBe("unlimited");
    expect(unitFor("process_virtual_memory_max_bytes", "gauge").format(1.8446744073709552e19))
      .toBe("unlimited");
  });

  it("scales a duration to where it actually falls", () => {
    const seconds = unitFor("go_gc_duration_seconds", "gauge");
    expect(seconds.format(0.0000508)).toBe("51µs");
    expect(seconds.format(0.00561)).toBe("5.61ms");
    expect(seconds.format(0.042)).toBe("42ms");
    expect(seconds.format(1.5)).toBe("1.50s");
  });

  it("treats a timestamp as a moment, and refuses a zero floor for it", () => {
    const stamp = unitFor("process_start_time_seconds", "gauge");
    expect(stamp.unit).toBe("timestamp");
    // Anchored at zero, every reading lands in a hairline above an axis
    // labelled with the 1970s.
    expect(stamp.anchorZero).toBe(false);
  });

  it("does not read a timestamp as a duration despite the suffix", () => {
    expect(unitFor("go_memstats_last_gc_time_seconds", "gauge").unit).toBe("timestamp");
    expect(unitFor("go_gc_duration_seconds", "gauge").unit).toBe("seconds");
  });

  it("marks the metrics whose content is in their labels", () => {
    expect(unitFor("go_info", "gauge").unit).toBe("info");
    expect(unitFor("octo_build_info", "gauge").unit).toBe("info");
    expect(unitFor("octo_flows", "gauge").unit).toBe("count");
  });

  it("shortens a count only once it stops being readable", () => {
    const count = unitFor("go_memstats_heap_objects", "gauge");
    expect(count.format(266_700)).toBe("266.7k");
    expect(count.format(15)).toBe("15");
  });
});
