/**
 * The NATS monitoring client — the middle layer between the queues server action
 * and the `fetch` abstraction (`@octo/http`):
 *
 *     serverAction (auth) → this client (getQueueStats()) → requestJson() → fetch
 *
 * Unlike the orchestrator client, the platform talks to the broker's monitoring
 * HTTP service (port 8222) directly: it is in-cluster reachable and the data is a
 * read-only snapshot, so there is no reason to hop through the orchestrator. The
 * server-only `NATS_MONITOR_URL` and the snake_case→camelCase shaping are internal;
 * callers see only the curated {@link QueueStats}.
 */

import { requestJson, type ActionResult } from "@octo/http";
import type {
  QueueConnection,
  QueueDestination,
  QueueServerStats,
  QueueStats,
  QueueSubscriber,
} from "@/app/model/queues";

/** The subset of NATS `/varz` we surface (snake_case as the broker emits it). */
interface Varz {
  server_name: string;
  version: string;
  now: string;
  uptime: string;
  connections: number;
  total_connections: number;
  in_msgs: number;
  out_msgs: number;
  in_bytes: number;
  out_bytes: number;
  slow_consumers: number;
  subscriptions: number;
}

/**
 * One subscription as NATS reports it under `?subs=detail`.
 *
 * `msgs` is the only per-subject counter the broker gives: everything else on a
 * connection (in/out messages and bytes, pending, subscription count) covers every
 * subject that client touches. Keeping that distinction is what stops two subjects
 * consumed by one client from reporting identical, meaningless numbers.
 */
interface SubDetail {
  subject: string;
  /** Queue-group name; present only for queue (load-balanced) subscriptions. */
  qgroup?: string;
  msgs: number;
}

/** The subset of NATS `/connz` we surface (fetched with `?subs=detail`). */
interface Connz {
  connections: Array<{
    cid: number;
    name?: string;
    subscriptions: number;
    pending_bytes: number;
    in_msgs: number;
    out_msgs: number;
    in_bytes: number;
    out_bytes: number;
    subscriptions_list_detail?: SubDetail[];
  }>;
}

// Platform queue subjects are scoped as `octo.<deployment>.q.<name>` and queue-
// subscribed on that same string (see runtime queues.go). Parse the readable parts.
const QUEUE_SUBJECT_RE = /^octo\.([^.]+)\.q\.(.+)$/;

/** Skip NATS/JetStream internal subjects (reply inboxes, system, JS) as non-queues. */
function isInternalSubject(subject: string): boolean {
  return subject.startsWith("_") || subject.startsWith("$");
}

/** The monitoring base URL with any trailing slash trimmed, or "" when unset. */
function baseUrl(): string {
  return (process.env.NATS_MONITOR_URL ?? "").replace(/\/+$/, "");
}

function toServer(v: Varz): QueueServerStats {
  return {
    serverName: v.server_name,
    version: v.version,
    now: v.now,
    uptime: v.uptime,
    connections: v.connections,
    totalConnections: v.total_connections,
    inMsgs: v.in_msgs,
    outMsgs: v.out_msgs,
    inBytes: v.in_bytes,
    outBytes: v.out_bytes,
    slowConsumers: v.slow_consumers,
    subscriptions: v.subscriptions,
  };
}

function toConnections(c: Connz): QueueConnection[] {
  return c.connections.map((conn) => ({
    cid: conn.cid,
    name: conn.name ?? "",
    subscriptions: conn.subscriptions,
    pending: conn.pending_bytes,
    inMsgs: conn.in_msgs,
    outMsgs: conn.out_msgs,
    inBytes: conn.in_bytes,
    outBytes: conn.out_bytes,
  }));
}

/**
 * Roll the per-connection subscription detail up into destinations: one row per
 * subject, carrying its subscription/message totals and the clients consuming it.
 * Internal subjects (reply inboxes, system) are dropped — they aren't queues.
 * `connections` indexes the full connection objects by cid.
 *
 * A subscriber's own counters are summed from the subscriptions that client holds
 * *on this subject*; its connection is carried alongside rather than merged in,
 * because those totals span every subject the client touches. Attaching them to a
 * subject was how two destinations consumed by one client came to report the same
 * numbers.
 */
function toDestinations(
  c: Connz,
  connections: QueueConnection[],
): QueueDestination[] {
  const byCid = new Map(connections.map((conn) => [conn.cid, conn]));
  const bySubject = new Map<string, QueueDestination>();
  // subject -> cid -> the subscriber row being accumulated for that pair.
  const subscribers = new Map<string, Map<number, QueueSubscriber>>();
  for (const conn of c.connections) {
    for (const sub of conn.subscriptions_list_detail ?? []) {
      if (isInternalSubject(sub.subject)) continue;
      let dest = bySubject.get(sub.subject);
      if (!dest) {
        const m = QUEUE_SUBJECT_RE.exec(sub.subject);
        dest = {
          subject: sub.subject,
          queue: sub.qgroup ?? null,
          deployment: m?.[1] ?? null,
          name: m?.[2] ?? sub.subject,
          subscriptions: 0,
          msgs: 0,
          subscribers: [],
        };
        bySubject.set(sub.subject, dest);
        subscribers.set(sub.subject, new Map());
      }
      dest.subscriptions += 1;
      dest.msgs += sub.msgs;

      const full = byCid.get(conn.cid);
      if (!full) continue;
      const perCid = subscribers.get(sub.subject)!;
      let row = perCid.get(conn.cid);
      if (!row) {
        row = {
          cid: conn.cid,
          name: full.name,
          queue: sub.qgroup ?? null,
          subscriptions: 0,
          msgs: 0,
          connection: full,
        };
        perCid.set(conn.cid, row);
        dest.subscribers.push(row);
      }
      row.subscriptions += 1;
      row.msgs += sub.msgs;
    }
  }
  return [...bySubject.values()].sort((a, b) => a.name.localeCompare(b.name));
}

/**
 * Pull a broker snapshot: `/varz` for the totals and `/connz` for the open
 * connections, fetched together. Returns an error result when monitoring is
 * unconfigured (`NATS_MONITOR_URL` unset) or either endpoint is unreachable.
 */
export async function getQueueStats(): Promise<ActionResult<QueueStats>> {
  const base = baseUrl();
  if (!base) {
    return {
      ok: false,
      error: "queue monitoring not configured (NATS_MONITOR_URL unset)",
    };
  }
  const [varz, connz] = await Promise.all([
    requestJson<Varz>("GET", `${base}/varz`),
    requestJson<Connz>("GET", `${base}/connz?subs=detail`),
  ]);
  if (!varz.ok) return varz;
  if (!connz.ok) return connz;
  const connections = toConnections(connz.data);
  return {
    ok: true,
    data: {
      server: toServer(varz.data),
      destinations: toDestinations(connz.data, connections),
    },
  };
}
