/**
 * The platform transport: drives the RUN server actions (`app/actions/run.ts`)
 * and streams logs over SSE. Implements the editor's RunTransport contract so the
 * shared RunProvider can drive it. The log stream stays an EventSource — server
 * actions can't back streaming — pointed at the surviving `/api/run/logs` route.
 *
 * Every call carries this tab's id, which the server mixes with the run cookie to
 * pick the tab's own runner. `runTabId()` is called inside each method rather than
 * once at module scope: this module is imported by a client component that is still
 * server-rendered, where there is no sessionStorage to read.
 */

import type {
  CelEvalRequest,
  CelEvalResult,
  FlowRunOutcome,
  FlowRunRequest,
  RunStatusSnapshot,
  RunTransport,
  TestRunOutcome,
  TestRunRequest,
} from "@octo/editor";
import { runTabId } from "@octo/editor";
import {
  runEvalCel,
  runInvoke,
  runStart,
  runStatus,
  runStop,
  runSync,
  runTest,
} from "@/app/actions/run";
import { unwrap } from "@/app/model/bff";

export const bffRunTransport: RunTransport = {
  async status(): Promise<RunStatusSnapshot> {
    return unwrap(await runStatus(await runTabId()));
  },

  async start({ yaml, integrationId }): Promise<RunStatusSnapshot> {
    return unwrap(await runStart(await runTabId(), yaml, integrationId));
  },

  async stop() {
    unwrap(await runStop(await runTabId()));
  },

  async sync({ yaml, integrationId }) {
    unwrap(await runSync(await runTabId(), yaml, integrationId));
  },

  async invoke(req: FlowRunRequest): Promise<FlowRunOutcome> {
    return unwrap(await runInvoke(await runTabId(), req));
  },

  async evalCel(req: CelEvalRequest): Promise<CelEvalResult> {
    return unwrap(await runEvalCel(await runTabId(), req));
  },

  async test(req: TestRunRequest): Promise<TestRunOutcome> {
    return unwrap(await runTest(await runTabId(), req));
  },

  // The only method that can't await up front — its disposer is returned
  // synchronously — so the stream opens once the id resolves, and unsubscribing
  // before that just makes sure it never opens.
  subscribeLogs(onLine) {
    let es: EventSource | null = null;
    let closed = false;
    void runTabId().then((tab) => {
      if (closed) return;
      es = new EventSource(`/api/run/logs?tab=${encodeURIComponent(tab)}`);
      es.onmessage = (ev) => {
        const seq = Number(ev.lastEventId);
        onLine(seq, ev.data);
      };
    });
    return () => {
      closed = true;
      es?.close();
    };
  },
};
