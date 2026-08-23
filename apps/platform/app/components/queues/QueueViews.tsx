"use client";

import type { QueueConnection, QueueSubscriber } from "@/app/model/queues";

// The tile and its formatters live in components/stats now, shared with the
// storage view: the two pages are read side by side when something is wrong, and a
// tile that rounded bytes differently on one of them would make two readings of the
// same install look like a discrepancy. Re-exported so existing importers of this
// module keep working.
export { Stat, bytes, num } from "@/app/components/stats/Stat";
import { bytes, num } from "@/app/components/stats/Stat";

const TH = "px-3 py-2 font-medium";
const THR = `${TH} text-right`;
const TD = "px-3 py-2 text-right tabular-nums";
const HEAD =
  "border-b border-black/10 text-left text-xs uppercase tracking-wide text-zinc-400 dark:border-white/10";

/**
 * The clients consuming one destination, and only what is true of this subject:
 * which connection, in which queue group, and how many messages its subscriptions
 * here were delivered.
 *
 * Deliberately not the connections' own totals. Those cover every subject a client
 * touches, so one client subscribed to two subjects shows the same set under
 * both — true, and unreadable as anything but a bug next to a per-subject message
 * count that differs. They are listed once each in ConnectionsTable; the CID is
 * what joins the two.
 */
export function SubscribersTable({
  subscribers,
}: {
  subscribers: QueueSubscriber[];
}) {
  return (
    <div className="overflow-x-auto rounded-xl border border-black/10 dark:border-white/10">
      <table className="w-full text-sm">
        <thead>
          <tr className={HEAD}>
            <th className={TH}>CID</th>
            <th className={TH}>Name</th>
            <th className={TH}>Queue group</th>
            <th className={THR}>Subs</th>
            <th className={THR}>Msgs</th>
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
              <td className={TD}>{num(s.subscriptions)}</td>
              <td className={TD}>{num(s.msgs)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/**
 * Every open client connection, once each, with the counters that belong to a
 * connection rather than to a subject.
 *
 * Published and Delivered, not "in" and "out". NATS reports a connection's
 * counters from the broker's side — `in_msgs` is what the client sent *to* it —
 * so a pure consumer reads zero in while the server's Messages in runs high, and
 * the two look like they disagree. Named for what the client did, and shown
 * beside the connection that published the traffic, they stop reading as a
 * contradiction: this client took 45, that one sent 55.
 */
export function ConnectionsTable({
  connections,
}: {
  connections: QueueConnection[];
}) {
  const group = "border-l border-black/10 dark:border-white/10";
  return (
    <div className="overflow-x-auto rounded-xl border border-black/10 dark:border-white/10">
      <table className="w-full text-sm">
        <thead className="text-xs uppercase tracking-wide text-zinc-400">
          <tr className="border-b border-black/5 text-left dark:border-white/5">
            <th className={TH} colSpan={4} />
            <th
              className={`${TH} ${group}`}
              colSpan={2}
              title="What this client sent to the broker. Across all connections this is the server's Messages in."
            >
              Published
            </th>
            <th
              className={`${TH} ${group}`}
              colSpan={2}
              title="What the broker delivered to this client. Across all connections this is the server's Messages out."
            >
              Delivered
            </th>
          </tr>
          <tr className={HEAD}>
            <th className={TH}>CID</th>
            <th className={TH}>Name</th>
            <th className={THR}>Subs</th>
            <th className={THR}>Pending</th>
            <th className={`${THR} ${group}`}>Msgs</th>
            <th className={THR}>Data</th>
            <th className={`${THR} ${group}`}>Msgs</th>
            <th className={THR}>Data</th>
          </tr>
        </thead>
        <tbody>
          {connections.map((c) => (
            <tr
              key={c.cid}
              className="border-b border-black/5 last:border-0 dark:border-white/5"
            >
              <td className="px-3 py-2 tabular-nums text-zinc-500">{c.cid}</td>
              <td className="max-w-xs truncate px-3 py-2" title={c.name}>
                {c.name || <span className="text-zinc-400">—</span>}
              </td>
              <td className={TD}>{num(c.subscriptions)}</td>
              <td className={TD}>{bytes(c.pending)}</td>
              <td className={`${TD} ${group}`}>{num(c.inMsgs)}</td>
              <td className={TD}>{bytes(c.inBytes)}</td>
              <td className={`${TD} ${group}`}>{num(c.outMsgs)}</td>
              <td className={TD}>{bytes(c.outBytes)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
