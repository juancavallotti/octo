"use client";

import { useCallback, useEffect, useState } from "react";
import { useConfirm } from "@/app/components/ConfirmDialog";
import {
  getRetention,
  runRetention,
  saveRetention,
  type RetentionPolicy,
  type RetentionRun,
} from "@/app/model/retention";
import { Field, INPUT, PrimaryButton } from "./fields";

/**
 * How long this installation keeps logs and traces, and a way to enforce it now.
 *
 * Two windows rather than one because the streams have very different weights: a
 * ten-block flow emits a couple of dozen trace records, each able to carry a
 * captured body, where the same request produces a log line or two.
 *
 * Zero means keep forever and is the default, so an installation that never
 * visits this page behaves as it always did. That is also why the field renders
 * its meaning in words — a bare 0 in a box labelled "days" reads as a mistake
 * rather than as a decision.
 */

/** The maximum the aggregator will store, mirrored here so the form says so first. */
const MAX_DAYS = 3650;

/** A window as the form holds it: a string, so the field can be empty mid-edit. */
type Draft = { logs: string; traces: string };

function toDraft(p: RetentionPolicy): Draft {
  return { logs: String(p.logsDays), traces: String(p.tracesDays) };
}

/** Parse one field. Returns null when it is not a window that can be stored. */
function parseDays(raw: string): number | null {
  const trimmed = raw.trim();
  if (!/^\d+$/.test(trimmed)) return null;
  const days = Number(trimmed);
  return days > MAX_DAYS ? null : days;
}

/** What a stored window means, in words. */
function describe(days: number): string {
  if (days === 0) return "Kept forever — nothing is deleted.";
  if (days === 1) return "Deleted the day after it is recorded.";
  return `Deleted after ${days} days.`;
}

/** One line summarising a sweep, in the order the tables matter. */
function describeRun(run: RetentionRun): string {
  const parts = [
    `${run.logsDeleted.toLocaleString()} log ${run.logsDeleted === 1 ? "event" : "events"}`,
    `${run.tracesDeleted.toLocaleString()} trace ${run.tracesDeleted === 1 ? "record" : "records"}`,
    `${run.traceSummariesDeleted.toLocaleString()} ${run.traceSummariesDeleted === 1 ? "trace" : "traces"}`,
  ];
  return `Deleted ${parts.join(", ")}.`;
}

export default function RetentionSettingsManager() {
  const confirm = useConfirm();
  const [policy, setPolicy] = useState<RetentionPolicy | null>(null);
  const [draft, setDraft] = useState<Draft>({ logs: "0", traces: "0" });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);
  const [lastRun, setLastRun] = useState<RetentionRun | null>(null);

  const apply = useCallback((next: RetentionPolicy) => {
    setPolicy(next);
    setDraft(toDraft(next));
  }, []);

  // A promise chain rather than an async body, so nothing sets state in the
  // synchronous part of the effect below — the form the other managers use.
  const load = useCallback(
    () => getRetention().then(apply, (e) => setError((e as Error).message)),
    [apply],
  );

  useEffect(() => {
    load();
  }, [load]);

  /** Run a mutation, then refresh; surface failures inline. */
  const run = useCallback(
    async (fn: () => Promise<unknown>) => {
      setBusy(true);
      setError(null);
      // Both outcome messages are cleared up front, not just on success. A sweep's
      // report is about the sweep that produced it, so leaving it up through the
      // next action would pair a stale "deleted 120 events" with whatever that
      // action reported — including an error.
      setSaved(false);
      setLastRun(null);
      try {
        await fn();
        await load();
      } catch (e) {
        setError((e as Error).message);
      } finally {
        setBusy(false);
      }
    },
    [load],
  );

  const logsDays = parseDays(draft.logs);
  const tracesDays = parseDays(draft.traces);

  // Gated on the policy having loaded, not just on the fields parsing. The form
  // seeds itself with zeros before the request resolves, so without this a fast
  // click saves "keep everything" over whatever was stored.
  const canSave =
    policy !== null && !busy && logsDays !== null && tracesDays !== null;

  const dirty =
    policy !== null &&
    (logsDays !== policy.logsDays || tracesDays !== policy.tracesDays);

  const save = () => {
    if (!canSave) return;
    run(async () => {
      await saveRetention({ logsDays: logsDays!, tracesDays: tracesDays! });
      setSaved(true);
    });
  };

  // A sweep enforces what is stored, not what is on screen. Rather than quietly
  // deleting to a policy the operator can see they have edited, the button waits
  // for the save — otherwise the numbers in front of them would not be the ones
  // the confirmation is about.
  const sweeps = policy !== null && (policy.logsDays > 0 || policy.tracesDays > 0);
  const canRun = policy !== null && !busy && !dirty && sweeps;

  const runNow = async () => {
    if (!canRun || policy === null) return;
    const scope = [
      policy.logsDays > 0 ? `log events older than ${policy.logsDays} days` : null,
      policy.tracesDays > 0 ? `traces older than ${policy.tracesDays} days` : null,
    ]
      .filter(Boolean)
      .join(" and ");
    const ok = await confirm({
      title: "Delete everything past its retention?",
      body: `This deletes ${scope}, now. A trace is deleted with all of its records. Nothing here can be undone.`,
      confirmLabel: "Delete now",
      danger: true,
    });
    if (!ok) return;
    run(async () => {
      setLastRun(await runRetention());
    });
  };

  return (
    <div className="flex h-full flex-col overflow-y-auto px-6 py-5">
      <div className="mx-auto w-full max-w-2xl">
        <h1 className="text-lg font-semibold">Data retention</h1>
        <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">
          How long this installation keeps stored logs and traces. A job enforces
          this nightly; nothing is deleted until you set a window.
        </p>

        {error && <p className="mt-3 text-sm text-red-500">{error}</p>}
        {saved && !error && (
          <p className="mt-3 text-sm text-emerald-600 dark:text-emerald-400">
            Policy saved.
          </p>
        )}
        {lastRun && !error && (
          <p className="mt-3 text-sm text-emerald-600 dark:text-emerald-400">
            {describeRun(lastRun)}
          </p>
        )}

        <div className="mt-4 flex flex-col gap-3 rounded-lg border border-black/10 p-4 dark:border-white/10">
          <Field
            label="Keep logs for (days)"
            hint={logsDays === null ? undefined : describe(logsDays)}
          >
            <input
              value={draft.logs}
              disabled={busy}
              inputMode="numeric"
              aria-label="Keep logs for (days)"
              onChange={(e) => setDraft((d) => ({ ...d, logs: e.target.value }))}
              className={`${INPUT} w-full font-mono`}
            />
          </Field>

          <Field
            label="Keep traces for (days)"
            hint={tracesDays === null ? undefined : describe(tracesDays)}
          >
            <input
              value={draft.traces}
              disabled={busy}
              inputMode="numeric"
              aria-label="Keep traces for (days)"
              onChange={(e) => setDraft((d) => ({ ...d, traces: e.target.value }))}
              className={`${INPUT} w-full font-mono`}
            />
          </Field>

          {(logsDays === null || tracesDays === null) && (
            <p className="text-xs text-amber-600 dark:text-amber-400">
              A window is a whole number of days, from 0 (keep forever) to{" "}
              {MAX_DAYS.toLocaleString()}.
            </p>
          )}

          <PrimaryButton onClick={save} disabled={!canSave} />
        </div>

        <div className="mt-4 flex flex-col gap-2 rounded-lg border border-black/10 p-4 dark:border-white/10">
          <h2 className="text-sm font-medium">Run now</h2>
          <p className="text-xs text-zinc-500 dark:text-zinc-400">
            Applies the saved policy immediately instead of waiting for tonight.
            Traces are deleted whole — a trace past its window takes its records
            with it — and stored model prices are never deleted.
          </p>
          <button
            type="button"
            onClick={runNow}
            disabled={!canRun}
            className="self-start rounded-md bg-red-600 px-3 py-1 text-sm font-medium text-white transition-colors hover:bg-red-500 disabled:opacity-50"
          >
            Delete now
          </button>
          {dirty && (
            <p className="text-xs text-zinc-500">
              Save your changes first — a sweep applies the stored policy, not
              what is on screen.
            </p>
          )}
          {!dirty && policy !== null && !sweeps && (
            <p className="text-xs text-zinc-500">
              Nothing to delete: both streams are set to be kept forever.
            </p>
          )}
        </div>
      </div>
    </div>
  );
}
