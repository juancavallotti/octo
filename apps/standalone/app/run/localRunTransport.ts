/**
 * The standalone transport: drives the RUN server actions (`app/actions/run.ts`,
 * backed by @octo/run-host spawning the bundled `octo` binary) and streams logs
 * over SSE. The log stream stays an EventSource — server actions can't back
 * streaming — pointed at the surviving `/api/run/logs` route.
 */

import type {
  CelEvalRequest,
  CelEvalResult,
  RunStatusSnapshot,
  RunTransport,
} from "@octo/editor";
import { runEvalCel, runStart, runStatus, runStop, runSync } from "../actions/run";
import { unwrap } from "../actions/result";

export const localRunTransport: RunTransport = {
  async status(): Promise<RunStatusSnapshot> {
    return unwrap(await runStatus());
  },

  async start({ yaml }): Promise<RunStatusSnapshot> {
    return unwrap(await runStart(yaml));
  },

  async stop() {
    unwrap(await runStop());
  },

  async sync({ yaml }) {
    unwrap(await runSync(yaml));
  },

  async evalCel(req: CelEvalRequest): Promise<CelEvalResult> {
    return unwrap(await runEvalCel(req));
  },

  subscribeLogs(onLine) {
    const es = new EventSource("/api/run/logs");
    es.onmessage = (ev) => {
      const seq = Number(ev.lastEventId);
      onLine(seq, ev.data);
    };
    return () => es.close();
  },
};
