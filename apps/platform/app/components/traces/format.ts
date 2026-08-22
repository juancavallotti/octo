/**
 * Rendering the three quantities a trace is read in: how long, how much, and how
 * long ago.
 *
 * Duration and cost both span many orders of magnitude here — a `set-variable`
 * takes tens of nanoseconds and a model call takes seconds; one token of a cheap
 * model costs $7.5×10⁻⁸ and an agent run costs cents — so neither can be rendered
 * with a fixed number of decimals. A fixed two-decimal dollar figure would print
 * `$0.00` for a real charge, which is the one thing the whole cost path exists to
 * avoid.
 */

import type { CostStatus } from "@/app/model/traces";

const MINUTE_NS = 60_000_000_000;

/** A duration in nanoseconds, at a scale a reader can hold. */
export function formatDuration(ns: number): string {
  if (!Number.isFinite(ns) || ns < 0) return "—";
  if (ns < 1_000) return `${Math.round(ns)}ns`;
  if (ns < 1_000_000) return `${trim(ns / 1_000)}µs`;
  if (ns < 1_000_000_000) return `${trim(ns / 1_000_000)}ms`;
  if (ns < MINUTE_NS) return `${trim(ns / 1_000_000_000)}s`;

  const totalSeconds = Math.round(ns / 1_000_000_000);
  const minutes = Math.floor(totalSeconds / 60);
  return `${minutes}m ${totalSeconds - minutes * 60}s`;
}

/** Three significant figures, without a trailing `.0`. */
function trim(value: number): string {
  const digits = value >= 100 ? 0 : value >= 10 ? 1 : 2;
  return String(Number(value.toFixed(digits)));
}

/**
 * A token count, at a scale a reader can compare at a glance.
 *
 * Counts here run from a handful to millions, and the interesting comparison is
 * almost always between two of them — input against output, this trace against
 * the last. Grouping separators make that comparison work at four figures;
 * beyond that the leading digits are the whole of what a reader takes in, so the
 * tail is dropped rather than rendered.
 */
export function formatTokens(count: number): string {
  if (!Number.isFinite(count) || count < 0) return "—";
  if (count < 10_000) return count.toLocaleString("en-US");
  if (count < 1_000_000) return `${trim(count / 1_000)}k`;
  return `${trim(count / 1_000_000)}M`;
}

/**
 * A dollar amount, with enough decimals to stay non-zero.
 *
 * A charge too small to show at the usual scale is rendered as `<$0.0001` rather
 * than rounded down to nothing: the number is small, but "this call was free" is
 * a different claim, and one the trace store took some care never to make.
 */
export function formatCost(usd: number): string {
  if (!Number.isFinite(usd)) return "—";
  if (usd === 0) return "$0";
  if (usd < 0.0001) return "<$0.0001";
  if (usd < 1) return `$${usd.toFixed(4)}`;
  return `$${usd.toFixed(2)}`;
}

/**
 * What a cost figure actually claims, given how many of its calls could be
 * priced. This is the one place the distinction is turned into words, so no
 * component has to remember to make it.
 *
 * With unpriced calls in the total, the figure is a lower bound and says so. A
 * total of zero with unpriced calls is not "free" at all — it is *nothing known*,
 * and rendering it as `$0` would be the exact misreading the store's
 * `cost_status` column exists to prevent.
 */
export function describeCost(
  usd: number,
  unpricedCalls: number,
  /**
   * Whether any model call was made at all. A trace knows this exactly; an app
   * row infers it from having a cost or an unpriced call, which is the same
   * question at that grain. Without it, "no model calls" and "model calls that
   * cost nothing" would render identically.
   */
  hasCalls: boolean,
): { text: string; partial: boolean; title: string } {
  if (!hasCalls && unpricedCalls === 0) {
    return { text: "—", partial: false, title: "No model calls" };
  }
  if (unpricedCalls === 0) {
    return { text: formatCost(usd), partial: false, title: "Every model call was priced" };
  }
  const calls = `${unpricedCalls} call${unpricedCalls === 1 ? "" : "s"}`;
  const title =
    `${calls} could not be priced — the model is not in the price catalogue, or ` +
    `the provider reported no usage. The real cost is higher than this.`;
  if (usd === 0) return { text: "unpriced", partial: true, title };
  return { text: `≥ ${formatCost(usd)}`, partial: true, title };
}

/**
 * Why one model call's cost is what it is, in the words a reader needs to judge
 * whether they can trust the number beside it.
 *
 * The store records this per call rather than leaving it to be inferred, because
 * every one of these cases produces a number — or a blank — that looks exactly
 * like the others until you know which it is.
 */
export function describeCostStatus(status: CostStatus): string {
  switch (status) {
    case "reported":
      return (
        "What the provider charged for this call, as the provider reported it — " +
        "not an estimate from a rate card. It includes any per-request or " +
        "per-image charge a token count cannot reconstruct."
      );
    case "priced":
      return "Priced from the published rate for this model.";
    case "priced_partial":
      return (
        "Partly priced: the catalogue publishes no cache-read rate for this " +
        "model, so cache reads were charged at the input rate. An over-estimate " +
        "that says so, rather than cache reads charged at nothing."
      );
    case "unpriced_model":
      return (
        "The model is not in the price catalogue, so nothing here can say what " +
        "the call cost. That is not the same as it being free."
      );
    case "no_usage":
      return "The provider reported no token usage for this call, so there is nothing to price.";
    default:
      return "";
  }
}

/** A short "how long ago", for a column too narrow for a timestamp. */
export function formatAge(iso: string, now: number = Date.now()): string {
  const then = Date.parse(iso);
  if (Number.isNaN(then)) return "—";
  const seconds = Math.round((now - then) / 1000);
  if (seconds < 0) return "just now";
  if (seconds < 60) return `${seconds}s ago`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
  if (seconds < 86_400) return `${Math.floor(seconds / 3600)}h ago`;
  return `${Math.floor(seconds / 86_400)}d ago`;
}

/** The window a set of counts was measured over, in words. */
export function formatWindow(from: string, to: string): string {
  const spanMs = Date.parse(to) - Date.parse(from);
  if (!Number.isFinite(spanMs) || spanMs <= 0) return "";
  const hours = Math.round(spanMs / 3_600_000);
  if (hours < 1) return `last ${Math.max(Math.round(spanMs / 60_000), 1)} minutes`;
  if (hours < 48) return `last ${hours} hour${hours === 1 ? "" : "s"}`;
  return `last ${Math.round(hours / 24)} days`;
}
