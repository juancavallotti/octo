"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { ArrowLeft } from "lucide-react";
import { getWatch, type Watch, type WatchInput } from "@/app/model/alerts";
import { EvaluationHistory } from "./EvaluationHistory";
import { WatchEditor } from "./WatchEditor";
import { newWatch } from "./catalogue";

/** The id that means "one that does not exist yet". */
const NEW = "new";

/**
 * One watch: the definition on the left of the fold, what it has done below it.
 *
 * The history is not shown for an unsaved watch, because there is nothing to
 * show and an empty panel would read as "it has never fired" rather than "it
 * does not exist". Preview is what answers the question at that stage.
 */
export default function WatchPage({ watchId }: { watchId: string }) {
  const creating = watchId === NEW;
  const [initial, setInitial] = useState<WatchInput | null>(
    creating ? newWatch() : null,
  );
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (creating) return;
    let stopped = false;
    const load = async () => {
      try {
        const watch = await getWatch(watchId);
        if (!stopped) setInitial(toInput(watch));
      } catch (e) {
        if (!stopped) setError((e as Error).message);
      }
    };
    void load();
    return () => {
      stopped = true;
    };
  }, [watchId, creating]);

  return (
    <div className="flex h-full flex-col overflow-y-auto px-6 py-5">
      <div className="mx-auto w-full max-w-4xl">
        <Link
          href="/platform/metrics/alerts"
          className="flex w-fit items-center gap-1 text-xs text-zinc-500 hover:text-zinc-800 dark:text-zinc-400 dark:hover:text-zinc-200"
        >
          <ArrowLeft size={12} />
          All watches
        </Link>

        <h1 className="mt-2 text-lg font-semibold">
          {creating ? "New watch" : (initial?.name ?? "Watch")}
        </h1>

        {error && (
          <p
            role="alert"
            className="mt-4 rounded-lg bg-red-500/10 px-3 py-2 text-sm text-red-600 dark:text-red-400"
          >
            {error}
          </p>
        )}

        {initial === null ? (
          <p className="mt-4 text-sm text-zinc-500">Loading…</p>
        ) : (
          <div className="mt-4 flex flex-col gap-8">
            {/*
        Keyed on the watch, so navigating from one to another builds a fresh
        editor rather than reusing the one holding the previous watch's draft.
        WatchEditor seeds its state from `initial` once; a reused instance would
        keep the old definition while the new one loaded, and Save sends what the
        editor holds — writing one watch's contents over another's id.
      */}
            <WatchEditor
              key={watchId}
              initial={initial}
              watchId={creating ? null : watchId}
            />
            {!creating && <EvaluationHistory watchId={watchId} />}
          </div>
        )}
      </div>
    </div>
  );
}

/** Drop the fields the service assigns, so the editor holds only what it writes. */
function toInput(watch: Watch): WatchInput {
  return {
    name: watch.name,
    description: watch.description,
    enabled: watch.enabled,
    severity: watch.severity,
    combinator: watch.combinator,
    conditions: watch.conditions,
    actions: watch.actions,
    onNoData: watch.onNoData,
    stepSeconds: watch.stepSeconds,
    intervalSeconds: watch.intervalSeconds,
    forSeconds: watch.forSeconds,
    cooldownSeconds: watch.cooldownSeconds,
  };
}
