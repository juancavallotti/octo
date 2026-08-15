"use client";

import type { QueueSubscriber } from "@/app/model/queues";

/** Group digits for readability; counts and byte values can run large. */
export function num(n: number): string {
  return n.toLocaleString();
}

/** Humanize a byte count (1024-based) to a compact, readable string. */
export function bytes(n: number): string {
  if (n < 1024) return `${n} B`;
  const units = ["KiB", "MiB", "GiB", "TiB"];
  let v = n / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v >= 10 || i === 0 ? 0 : 1)} ${units[i]}`;
}

/** One headline counter tile. */
export function Stat({
  label,
  value,
  alert,
}: {
  label: string;
  value: string;
  /** Render the value in the alert color (e.g. nonzero slow consumers). */
  alert?: boolean;
}) {
  return (
    <div className="rounded-xl border border-black/10 bg-white/40 p-4 dark:border-white/10 dark:bg-zinc-900/30">
      <div className="text-xs text-zinc-500 dark:text-zinc-400">{label}</div>
      <div
        className={`mt-1 text-lg font-semibold tabular-nums ${
          alert ? "text-red-500" : ""
        }`}
      >
        {value}
      </div>
    </div>
  );
}

/**
 * The clients consuming one destination.
 *
 * Split into two column groups, because the broker reports at two levels and only
 * the first is about this subject: a subscription's messages are its own, while a
 * connection's counters cover every subject that client touches. One client
 * subscribed to two subjects shows identical connection totals under both — true,
 * and misread as a bug for as long as the two sat under one header.
 */
export function SubscribersTable({
  subscribers,
}: {
  subscribers: QueueSubscriber[];
}) {
  const th = "px-3 py-2 font-medium";
  const thr = `${th} text-right`;
  const td = "px-3 py-2 text-right tabular-nums";
  return (
    <div className="overflow-x-auto rounded-xl border border-black/10 dark:border-white/10">
      <table className="w-full text-sm">
        <thead className="text-xs uppercase tracking-wide text-zinc-400">
          <tr className="border-b border-black/5 text-left dark:border-white/5">
            <th className={th} colSpan={4}>
              On this destination
            </th>
            <th
              className={`${th} border-l border-black/10 dark:border-white/10`}
              colSpan={5}
              title="Counters for the whole client connection, across every subject it consumes — not just this one"
            >
              Connection totals (all subjects)
            </th>
          </tr>
          <tr className="border-b border-black/10 text-left dark:border-white/10">
            <th className={th}>CID</th>
            <th className={th}>Name</th>
            <th className={th}>Queue group</th>
            <th className={thr}>Msgs</th>
            <th
              className={`${thr} border-l border-black/10 dark:border-white/10`}
            >
              Subs
            </th>
            <th className={thr}>Pending</th>
            <th className={thr}>Msgs in</th>
            <th className={thr}>Msgs out</th>
            <th className={thr}>Data out</th>
          </tr>
        </thead>
        <tbody>
          {subscribers.map((s) => (
            <tr
              key={s.cid}
              className="border-b border-black/5 last:border-0 dark:border-white/5"
            >
              <td className="px-3 py-2 tabular-nums text-zinc-500">{s.cid}</td>
              <td className="max-w-xs truncate px-3 py-2" title={s.name}>
                {s.name || <span className="text-zinc-400">—</span>}
              </td>
              <td className="max-w-xs truncate px-3 py-2" title={s.queue ?? ""}>
                {s.queue ?? <span className="text-zinc-400">—</span>}
              </td>
              <td
                className={td}
                title={`over ${num(s.subscriptions)} subscription(s)`}
              >
                {num(s.msgs)}
              </td>
              <td
                className={`${td} border-l border-black/10 text-zinc-500 dark:border-white/10`}
              >
                {num(s.connection.subscriptions)}
              </td>
              <td className={`${td} text-zinc-500`}>
                {bytes(s.connection.pending)}
              </td>
              <td className={`${td} text-zinc-500`}>
                {num(s.connection.inMsgs)}
              </td>
              <td className={`${td} text-zinc-500`}>
                {num(s.connection.outMsgs)}
              </td>
              <td className={`${td} text-zinc-500`}>
                {bytes(s.connection.outBytes)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
