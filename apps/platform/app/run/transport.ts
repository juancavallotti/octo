/**
 * The platform transport: drives the RUN server actions (`app/actions/run.ts`)
 * and streams logs over SSE. Implements the editor's RunTransport contract so the
 * shared RunProvider can drive it. The log stream stays an EventSource — server
 * actions can't back streaming — pointed at the surviving `/api/run/logs` route.
 */

import type {
  CelEvalRequest,
  CelEvalResult,
  RunStatusSnapshot,
  RunTransport,
} from "@octo/editor";
import {
  runEvalCel,
  runStart,
  runStatus,
  runStop,
  runSync,
} from "@/app/actions/run";
import { unwrap } from "@/app/model/bff";

export const bffRunTransport: RunTransport = {
  async status(): Promise<RunStatusSnapshot> {
    return unwrap(await runStatus());
  },

  async start({ yaml, integrationId }): Promise<RunStatusSnapshot> {
    return unwrap(await runStart(yaml, integrationId));
  },

  async stop() {
    unwrap(await runStop());
  },

  async sync({ yaml, integrationId }) {
    unwrap(await runSync(yaml, integrationId));
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
