"use client";

import type { ContextGauge as Gauge } from "./turns";

/**
 * How full the conversation is, against how full it may get.
 *
 * The number is measured rather than estimated — the provider's own count of what
 * it read plus what it produced — and it arrives on every model turn, so this
 * fills as a conversation goes on. That is the point: the interesting moment is
 * the one *before* the agent has to shorten the conversation, and until now
 * nothing said it was coming.
 *
 * Hidden while there is room, because a bar that is always there is furniture. It
 * appears when it starts to matter.
 */

/** Below this the conversation has plenty of room and the gauge is noise. */
const SHOW_ABOVE = 0.5;

/** Above this the next turn may not fit, and shortening is imminent. */
const TIGHT = 0.85;

export default function ContextGauge({ gauge }: { gauge: Gauge }) {
  const filled = gauge.max > 0 ? gauge.used / gauge.max : 0;
  if (filled < SHOW_ABOVE) return null;

  const percent = Math.min(100, Math.round(filled * 100));
  return (
    <div
      className="flex items-center gap-1.5"
      title={`${gauge.used.toLocaleString()} of ${gauge.max.toLocaleString()} tokens of conversation`}
    >
      <div
        className="h-1 w-10 overflow-hidden rounded-full bg-black/10 dark:bg-white/15"
        role="progressbar"
        aria-label="Conversation context used"
        aria-valuenow={percent}
        aria-valuemin={0}
        aria-valuemax={100}
      >
        <div
          className={`h-full ${filled >= TIGHT ? "bg-amber-500" : "bg-zinc-400 dark:bg-zinc-500"}`}
          style={{ width: `${percent}%` }}
        />
      </div>
      <span className="font-mono text-[10px] tabular-nums text-zinc-500">{percent}%</span>
    </div>
  );
}
