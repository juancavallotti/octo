/**
 * The standalone transport: drives the RUN server actions (`app/actions/run.ts`,
 * backed by @octo/run-host spawning the bundled `octo` binary) and streams logs
 * over SSE. The log stream stays an EventSource — server actions can't back
 * streaming — pointed at the surviving `/api/run/logs` route.
 *
 * Every call carries this tab's id, which the server mixes with the run cookie to
 * pick the tab's own runner. `runTabId()` is called inside each method rather than
 * once at module scope: this module is imported by a client component that is still
 * server-rendered, where there is no sessionStorage to read.
 *
 * The contract's run target is deliberately unused: a run here is a child process
 * of this very process, keyed by the browser tab that asked for it, so the open
 * integration names nothing. `start`/`sync` take the integration only where it decides
 * which resources to stage, and standalone reads those from the workspace instead.
 */

import { runTabId } from "@octo/editor";
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
import {
  runEvalCel,
  runInvoke,
  runStart,
  runStatus,
  runStop,
  runSync,
  runTest,
} from "../actions/run";
import { unwrap } from "../actions/result";

export const localRunTransport: RunTransport = {
  async status(): Promise<RunStatusSnapshot> {
    return unwrap(await runStatus(await runTabId()));
  },

  async start({ yaml }): Promise<RunStatusSnapshot> {
    return unwrap(await runStart(await runTabId(), yaml));
  },

  async stop() {
    unwrap(await runStop(await runTabId()));
  },

  async sync({ yaml }) {
    unwrap(await runSync(await runTabId(), yaml));
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
    void runTabId()
      .then((tab) => {
        if (closed) return;
        es = new EventSource(`/api/run/logs?tab=${encodeURIComponent(tab)}`);
        es.onmessage = (ev) => {
          const seq = Number(ev.lastEventId);
          onLine(seq, ev.data);
        };
      })
      .catch(() => {
        // Opening the stream is the last thing that can fail here, and there is
        // nowhere to report it to: the panel stays empty rather than the failure
        // surfacing as an unhandled rejection.
      });
    return () => {
      closed = true;
      es?.close();
    };
  },
};
