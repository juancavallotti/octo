/**
 * What a metric's numbers mean, inferred from its name and kind.
 *
 * Prometheus has no unit metadata on the wire, and the pod stats dictionary
 * carries none either — so the convention in the name is all there is. That is
 * enough here because these are the runtime's own metrics plus the two standard
 * collectors, all of which follow it: a `_bytes` suffix is bytes, `_seconds` is
 * seconds, `_total` is a counter.
 *
 * Getting this wrong is not cosmetic. 133000000 rendered as a plain number is
 * unreadable where "127 MiB" is obvious, and a counter charted as its raw value
 * would show a line that only ever climbs.
 */

import { bytes, num } from "@/app/components/stats/Stat";
import type { MetricKind } from "@/app/model/stats";

export type Unit = "bytes" | "seconds" | "count" | "ratio" | "timestamp" | "info";

/**
 * Past this, a byte value is a sentinel rather than a size. Go writes
 * math.MaxInt64 for "no limit" in gomemlimit and the kernel does the same for an
 * unset rlimit, and "8388608 TiB" is a worse way of saying so than the word is.
 */
const UNLIMITED = 2 ** 62;

export interface MetricUnit {
  unit: Unit;
  /** Whether points are growth per interval rather than a reading. */
  rate: boolean;
  /** What the axis is measuring, for the card's subtitle. */
  label: string;
  format: (value: number) => string;
  /** Whether zero is a meaningful floor for this unit. */
  anchorZero?: boolean;
}

/**
 * A metric whose value is always 1 and whose content is in its labels —
 * `go_info`, `octo_build_info`. Charting a flat line at 1 says nothing; the
 * labels say the version and the build date, which is the entire point of the
 * metric.
 */
const INFO = /(^|_)(build_)?info$/;

/** An absolute moment, not a duration, despite the `_seconds` suffix. */
const TIMESTAMP = /_time_seconds$/;

export function unitFor(name: string, kind: MetricKind): MetricUnit {
  const rate = kind === "counter";

  if (INFO.test(name)) {
    return { unit: "info", rate: false, label: "", format: () => "" };
  }
  if (TIMESTAMP.test(name)) {
    return {
      unit: "timestamp",
      rate: false,
      label: "moment",
      format: (v) => new Date(v * 1000).toLocaleString(),
      // A unix timestamp charted from zero is a flat line at the top of an axis
      // labelled with the 1970s.
      anchorZero: false,
    };
  }
  if (name.endsWith("_bytes") || name.endsWith("_bytes_total")) {
    return {
      unit: "bytes",
      rate,
      label: rate ? "bytes/s" : "bytes",
      format: (v) =>
        v >= UNLIMITED ? "unlimited" : rate ? `${bytes(v)}/s` : bytes(v),
    };
  }
  if (name.endsWith("_seconds") || name.endsWith("_seconds_total")) {
    // Seconds accrued per second is a dimensionless occupancy — one core, one
    // in-flight request — so a rate of seconds is not "seconds per second" to
    // anybody reading it.
    return {
      unit: "seconds",
      rate,
      label: rate ? "in use" : "seconds",
      format: (v) => (rate ? v.toFixed(3) : seconds(v)),
    };
  }
  if (name.endsWith("_percent")) {
    return { unit: "ratio", rate: false, label: "percent", format: (v) => `${num(v)}%` };
  }
  return {
    unit: "count",
    rate,
    label: rate ? "per second" : "count",
    format: (v) => (rate ? `${compact(v)}/s` : compact(v)),
  };
}

/** A duration in seconds, at the scale it actually falls on. GC pauses are
 * microseconds and flow durations are seconds, on the same page. */
function seconds(value: number): string {
  const abs = Math.abs(value);
  if (abs === 0) return "0";
  if (abs < 1e-3) return `${(value * 1e6).toFixed(0)}µs`;
  if (abs < 1) return `${(value * 1e3).toFixed(abs < 0.01 ? 2 : 0)}ms`;
  if (abs < 60) return `${value.toFixed(2)}s`;
  return `${(value / 60).toFixed(1)}m`;
}

/** A count, shortened once it stops being readable digit by digit. */
function compact(value: number): string {
  const abs = Math.abs(value);
  if (abs >= 1e9) return `${(value / 1e9).toFixed(1)}G`;
  if (abs >= 1e6) return `${(value / 1e6).toFixed(1)}M`;
  if (abs >= 1e4) return `${(value / 1e3).toFixed(1)}k`;
  if (!Number.isInteger(value) && abs < 100) return value.toFixed(2);
  return num(Math.round(value));
}
