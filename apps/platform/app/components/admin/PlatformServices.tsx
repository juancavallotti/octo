"use client";

import { useCallback, useEffect, useState } from "react";
import { getHealth, type Dependency } from "@/app/model/health";
import { SecondaryButton } from "./fields";

/**
 * What this installation runs on, and whether the orchestrator can reach it.
 *
 * The only admin page that reports rather than configures. It exists because the
 * answer used to live in pod logs: when the platform is behaving strangely the
 * first question is which of the four processes underneath it is up, and getting
 * to it meant kubectl.
 *
 * It says what it checked, in as many words. A single round trip is all this
 * proves — Postgres answering says nothing about replication lag, Redis answering
 * says nothing about how full it is — and a page that implied more would be worse
 * than this one, because it would be believed.
 */

/** What each dependency is for, in the terms of what breaks without it. */
const WHAT_IT_DOES: Record<string, string> = {
  postgres: "Integrations, deployments, stored logs and traces. Nothing works without it.",
  redis: "Shared state between replicas — today, the fold that keeps streamed traces small.",
  nats: "Live updates: deployment status, and the telemetry runtimes ship to the aggregator.",
  kubernetes: "Deploying anything. Without it the platform is an editor and a database.",
};

/** Human name, since the wire uses the lowercase identifiers the probes do. */
const LABELS: Record<string, string> = {
  postgres: "Postgres",
  redis: "Redis",
  nats: "NATS",
  kubernetes: "Kubernetes",
};

/**
 * Three states, not two. "Not configured" is deliberately neutral rather than a
 * warning: running without cluster access is supported, and colouring it as a
 * fault would send someone looking for one that is not there.
 */
function Badge({ dep }: { dep: Dependency }) {
  if (!dep.configured) {
    return (
      <span className="shrink-0 rounded-full bg-zinc-500/15 px-2 py-0.5 text-xs text-zinc-600 dark:text-zinc-400">
        Not configured
      </span>
    );
  }
  if (dep.reachable) {
    return (
      <span className="shrink-0 rounded-full bg-emerald-500/15 px-2 py-0.5 text-xs text-emerald-600 dark:text-emerald-400">
        Reachable{dep.latencyMs ? ` · ${dep.latencyMs}ms` : ""}
      </span>
    );
  }
  return (
    <span className="shrink-0 rounded-full bg-red-500/15 px-2 py-0.5 text-xs text-red-600 dark:text-red-400">
      Unreachable
    </span>
  );
}

function ServiceRow({ dep }: { dep: Dependency }) {
  return (
    <div className="border-b border-black/[0.06] py-3 last:border-b-0 dark:border-white/[0.08]">
      <div className="flex items-baseline justify-between gap-4">
        <span className="text-sm font-medium">{LABELS[dep.name] ?? dep.name}</span>
        <Badge dep={dep} />
      </div>
      <p className="mt-1 text-xs text-zinc-500">{WHAT_IT_DOES[dep.name] ?? ""}</p>
      {/* The transport error, verbatim. It is what someone greps for, and
          paraphrasing it would only put a second string between them and it. */}
      {dep.detail && (
        <p className="mt-1 font-mono text-xs break-words text-red-500">{dep.detail}</p>
      )}
    </div>
  );
}

export default function PlatformServices() {
  const [report, setReport] = useState<Dependency[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Settled in the promise's callbacks rather than around the await, so the effect
  // below starts the request without writing state during its own body — the same
  // shape AgentSettingsManager's loader has, and for the same reason.
  const load = useCallback(
    () =>
      getHealth().then(
        (next) => {
          setReport(next.dependencies);
          setError(null);
        },
        (e) => {
          // Dropping the previous report rather than keeping it: a stale answer on
          // a page whose whole subject is what is up right now is worse than none.
          setReport(null);
          setError((e as Error).message);
        },
      ),
    [],
  );

  useEffect(() => {
    load();
  }, [load]);

  /** Ask again. The busy flag belongs here rather than in load: the first check
      renders as "Checking…" from having no report yet. */
  const recheck = () => {
    setBusy(true);
    load().finally(() => setBusy(false));
  };

  return (
    <div className="flex h-full flex-col overflow-y-auto px-6 py-5">
      <div className="mx-auto w-full max-w-2xl">
        <h1 className="text-lg font-semibold">Platform services</h1>
        <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">
          What this installation runs on, and whether the orchestrator can reach it
          right now. Each check is one round trip — enough to say a dependency
          answered, not enough to say it is healthy.
        </p>

        {error && <p className="mt-3 text-sm text-red-500">{error}</p>}

        <div className="mt-4">
          {report === null && !error ? (
            <p className="text-sm text-zinc-500">Checking…</p>
          ) : (
            report?.map((dep) => <ServiceRow key={dep.name} dep={dep} />)
          )}
        </div>

        {/* Manual rather than polling. The page is opened to answer a question, and
            a poll would keep four probes running against a cluster that is already
            having a bad day. */}
        <div className="mt-4">
          <SecondaryButton onClick={recheck} disabled={busy}>
            {busy ? "Checking…" : "Check again"}
          </SecondaryButton>
        </div>
      </div>
    </div>
  );
}
