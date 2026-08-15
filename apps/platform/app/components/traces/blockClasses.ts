/**
 * What kind of work a span was: waiting on something else, or doing something
 * here.
 *
 * This is the question a trace is usually opened to answer — "where did the two
 * seconds go?" — and it is presentation policy rather than runtime fact, because
 * the runtime does not record it. So it is a table, kept honest three ways:
 *
 *  - **`unclassified` is a real, visible bucket**, never folded into either of
 *    the other two. A block nobody has classified is unknown, and a chart that
 *    silently calls it CPU is worse than one that admits it does not know.
 *  - **A drift test** (`blockClasses.test.ts`) reads the block types out of the
 *    Go tree and fails when one of them is missing from this table. The proper
 *    fix is to put the classification in the block registry itself and have
 *    `octo schema` carry it — see the follow-up issue — so that a new connector
 *    is caught by a check rather than by a wrong chart.
 *  - **Tree position outranks the table.** A composite that wrapped real work is
 *    control whatever this table says, because its span is its children's.
 *
 * Two entries here disagree with the obvious reading and are worth naming: the
 * cache and agent-memory blocks look in-process, but the runtime's KV store is an
 * HTTP call to the orchestrator on a deployment (`runtime/services/k8s/kv.go`),
 * so their time is spent waiting on the network like any other request.
 */

import { unionLength, type Interval } from "./timeSpans";
import { MODEL_KINDS, type Waterfall, type WaterfallNode } from "./types";

/** What a span's time was spent on. */
export type WorkClass = "io" | "cpu" | "control" | "unclassified";

/**
 * Every block type the runtime registers, by what it spends its time on.
 *
 * `io` leaves the process. `cpu` runs here. `control` is a composite whose own
 * time is its children's. Anything absent is `unclassified` and says so.
 */
export const BLOCK_CLASSES: Record<string, WorkClass> = {
  // --- leaves the process ---------------------------------------------------
  rest: "io",
  "rest-dynamic": "io",
  sql: "io",
  "queue-dispatch": "io",
  "publish-event": "io",
  // Writes a frame to a client's open stream.
  "sse-event": "io",
  "object-read": "io",
  "object-write": "io",
  "object-delete": "io",
  // The KV store is an HTTP round trip to the orchestrator, so evicting an entry
  // and wiping a thread are both network waits rather than local bookkeeping.
  "invalidate-cache": "io",
  "clear-agent-memory": "io",
  // A cache-scope that hit runs nothing inside it: its whole span is the KV read.
  // On a miss it has children, and tree position makes it control regardless.
  "cache-scope": "io",
  "pinecone-query": "io",
  "pinecone-upsert": "io",
  "pinecone-fetch": "io",
  "pinecone-delete": "io",
  "mongodb-find": "io",
  "mongodb-insert": "io",
  "mongodb-update": "io",
  "mongodb-delete": "io",
  "mongodb-aggregate": "io",
  "slack-send-message": "io",
  "slack-update-message": "io",
  "slack-add-reaction": "io",
  "slack-lookup-user": "io",
  "notion-query-datasource": "io",
  "notion-retrieve-page": "io",
  "notion-retrieve-blocks": "io",
  // Both call a provider. Their own llm.* records nest inside them, at which
  // point tree position takes over and the model call carries the time.
  "ai-embed": "io",
  "ai-mapping": "io",

  // --- runs in this process -------------------------------------------------
  "multi-transform": "cpu",
  "set-payload": "cpu",
  "set-variable": "cpu",
  "delete-variable": "cpu",
  log: "cpu",
  validate: "cpu",
  "template-resource": "cpu",
  // Signature checks: HMAC over the request, no network.
  "slack-verify-request": "cpu",
  "notion-verify-request": "cpu",
  // Normalize and filter an already-verified webhook payload.
  "slack-event": "cpu",
  "notion-event": "cpu",
  // Renders a blocks array that another block already fetched.
  "notion-page-to-markdown": "cpu",
  // Per invocation this is signature verification. Its discover/jwks modes do
  // reach the network, but once — the key set is fetched lazily and cached — so
  // classing the block by that one call would misread every later request.
  "jwt-validate": "cpu",

  // --- holds other work -----------------------------------------------------
  fork: "control",
  if: "control",
  switch: "control",
  foreach: "control",
  enrich: "control",
  "handle-errors": "control",
  split: "control",
  aggregate: "control",
  "flow-ref": "control",
  "ai-agent": "control",
  "ai-router": "control",
  "ai-retry": "control",
  "mcp-router": "control",
  // A cli-run holds a child process and, when it has an events path, the
  // sub-flow it runs per output line. Its own span is almost entirely the wait
  // on that child, which is work happening outside this process — the same
  // reason `rest` and `sql` are io rather than cpu.
  "cli-run": "io",
  // Injected by `octo invoke --break-at/--spies/--mocks`, never authored, and
  // never present in a deployed pod. Classified so a dev-run trace reads.
  breakpoint: "control",
  spy: "control",
  mock: "control",
};

/** What a block type spends its time on; `unclassified` when nobody has said. */
export function classifyBlock(blockType: string): WorkClass {
  return BLOCK_CLASSES[blockType] ?? "unclassified";
}

/**
 * What one span's time was spent on.
 *
 * A model call is classified by its **record kind** rather than by the block that
 * made it: whichever block was calling, the time went to a provider. And a span
 * with children is control by position — its extent is its children's, so
 * counting it too would bill the same nanoseconds twice.
 */
export function classifySpan(node: WaterfallNode): WorkClass {
  if (MODEL_KINDS.has(node.kind)) return "io";
  if (node.children.length > 0) return "control";
  // A flow terminal or a source response holds work by definition, even when its
  // records did not survive to show it.
  if (node.path === "") return "control";
  return classifyBlock(node.record?.blockType ?? "");
}

/** Where a trace's time went. */
export interface WorkBreakdown {
  /** The whole trace, end to end. */
  totalNs: number;
  /** Wall-clock time with at least one span of that class in flight. */
  ioNs: number;
  cpuNs: number;
  unclassifiedNs: number;
  /** Wall-clock covered by any classified leaf at all. */
  coveredNs: number;
  /** `totalNs - coveredNs`: engine overhead, queueing, and the composites that
   * held nothing. Shown explicitly, because it is what stops the other buckets
   * from silently absorbing the time nobody measured. */
  unattributedNs: number;
  /** The block types that fell through the table, for the panel to name. */
  unknownTypes: string[];
  /**
   * True when spans of different classes overlapped — a fork with a `rest` in one
   * branch and a transform in another. The buckets then sum to more than
   * `coveredNs`, because each is wall-clock rather than a share of it, and a
   * stacked bar drawn from them would be a lie about the arithmetic.
   */
  concurrent: boolean;
}

/**
 * Sum a trace's leaf spans by class.
 *
 * Leaves only, and the **union** of their intervals rather than the sum of their
 * durations: four fork branches each waiting 100ms on a REST call spent 100ms of
 * the trace's time between them, not 400ms. Summing would let one branch account
 * for more time than the whole trace lasted.
 */
export function workBreakdown(waterfall: Waterfall): WorkBreakdown {
  const spans: Record<string, Interval[]> = { io: [], cpu: [], unclassified: [] };
  const unknown = new Set<string>();

  const walk = (nodes: WaterfallNode[]) => {
    for (const node of nodes) {
      if (node.children.length > 0) {
        walk(node.children);
        continue;
      }
      const workClass = classifySpan(node);
      if (workClass === "control") continue;
      if (workClass === "unclassified" && node.record?.blockType) {
        unknown.add(node.record.blockType);
      }
      spans[workClass].push({ start: node.start, end: node.end });
    }
  };
  walk(waterfall.roots);

  const ioNs = unionLength(spans.io);
  const cpuNs = unionLength(spans.cpu);
  const unclassifiedNs = unionLength(spans.unclassified);
  const coveredNs = unionLength([
    ...spans.io,
    ...spans.cpu,
    ...spans.unclassified,
  ]);

  return {
    totalNs: waterfall.spanNs,
    ioNs,
    cpuNs,
    unclassifiedNs,
    coveredNs,
    unattributedNs: Math.max(waterfall.spanNs - coveredNs, 0),
    unknownTypes: [...unknown].sort(),
    concurrent: ioNs + cpuNs + unclassifiedNs > coveredNs,
  };
}
