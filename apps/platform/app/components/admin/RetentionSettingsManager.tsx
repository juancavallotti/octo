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
import { PrimaryButton } from "./fields";
import { RetentionWindows, type Draft } from "./RetentionWindows";
import {
  describe,
  describeRun,
  describeScope,
  MAX_DAYS,
  parseDays,
  toDraft,
} from "./retentionText";

/**
 * How long this installation keeps logs, traces and alerting history, and a way
 * to enforce it now.
 *
 * Three windows rather than one because the streams have very different weights:
 * a ten-block flow emits a couple of dozen trace records, each able to carry a
 * captured body, where the same request produces a log line or two — and a watch
 * writes an evaluation every time it runs whether or not anything happened.
 *
 * Zero means keep forever, and on the two evidence streams it is also the
 * default, so an installation that never visits this page behaves as it always
 * did. The alerting window is the exception and defaults to a real number; the
 * fields render their meaning in words for exactly that reason — a bare 0 in a
 * box labelled "days" reads as a mistake rather than as a decision.
 */

export default function RetentionSettingsManager() {
  const confirm = useConfirm();
  const [policy, setPolicy] = useState<RetentionPolicy | null>(null);
  const [draft, setDraft] = useState<Draft>({
    logs: "0",
    traces: "0",
    alerts: "0",
  });
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
  const alertsDays = parseDays(draft.alerts);

  // Gated on the policy having loaded, not just on the fields parsing. The form
  // seeds itself with zeros before the request resolves, so without this a fast
  // click saves "keep everything" over whatever was stored.
  const canSave =
    policy !== null &&
    !busy &&
    logsDays !== null &&
    tracesDays !== null &&
    alertsDays !== null;

  const dirty =
    policy !== null &&
    (logsDays !== policy.logsDays ||
      tracesDays !== policy.tracesDays ||
      alertsDays !== policy.alertsDays);

  const save = () => {
    if (!canSave) return;
    run(async () => {
      await saveRetention({
        logsDays: logsDays!,
        tracesDays: tracesDays!,
        alertsDays: alertsDays!,
      });
      setSaved(true);
    });
  };

  // A sweep enforces what is stored, not what is on screen. Rather than quietly
  // deleting to a policy the operator can see they have edited, the button waits
  // for the save — otherwise the numbers in front of them would not be the ones
  // the confirmation is about.
  const sweeps =
    policy !== null &&
    (policy.logsDays > 0 || policy.tracesDays > 0 || policy.alertsDays > 0);
  const canRun = policy !== null && !busy && !dirty && sweeps;

  const runNow = async () => {
    if (!canRun || policy === null) return;
    const scope = describeScope(policy);
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
          How long this installation keeps stored logs and traces. A job
          enforces this nightly; nothing is deleted until you set a window.
        </p>

        {/* Live regions, because every one of these appears after an await
            rather than in response to a keystroke — without them a screen
            reader user presses Delete now, hears nothing, and has no way to
            tell a sweep that deleted 40 000 rows from one that was refused. */}
        {error && (
          <p role="alert" className="mt-3 text-sm text-red-500">
            {error}
          </p>
        )}
        {saved && !error && (
          <p
            role="status"
            className="mt-3 text-sm text-emerald-600 dark:text-emerald-400"
          >
            Policy saved.
          </p>
        )}
        {lastRun && !error && (
          <p
            role="status"
            className="mt-3 text-sm text-emerald-600 dark:text-emerald-400"
          >
            {describeRun(lastRun)}
          </p>
        )}

        <div className="mt-4 flex flex-col gap-3 rounded-lg border border-black/10 p-4 dark:border-white/10">
          <RetentionWindows
            draft={draft}
            parsed={{ logs: logsDays, traces: tracesDays, alerts: alertsDays }}
            busy={busy}
            describe={describe}
            onChange={(window, value) =>
              setDraft((d) => ({ ...d, [window]: value }))
            }
          />

          {(logsDays === null ||
            tracesDays === null ||
            alertsDays === null) && (
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
