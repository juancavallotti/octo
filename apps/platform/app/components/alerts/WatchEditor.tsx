"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { useConfirm } from "@/app/components/ConfirmDialog";
import { Field, INPUT } from "@/app/components/admin/fields";
import {
  createWatch,
  deleteWatch,
  previewWatch,
  saveWatch,
  type WatchInput,
  type WatchPreview,
} from "@/app/model/alerts";
import { ActionList } from "./ActionRow";
import { ConditionList } from "./ConditionList";
import { PreviewPanel } from "./PreviewPanel";
import { ScheduleFields } from "./ScheduleFields";

/**
 * Write a watch: name it, say what to ask, say when to ask it, say who to tell.
 *
 * The Preview button is the part worth having. Tuning a spike against a
 * definition you cannot run is guesswork, and the alternative to guessing is
 * saving it and waiting to find out that it never fires — so this runs the real
 * evaluation against real history, records nothing, and shows every condition's
 * number against the threshold it was judged by.
 */
export function WatchEditor({
  initial,
  watchId,
}: {
  initial: WatchInput;
  /** Null for a watch that has not been created yet. */
  watchId: string | null;
}) {
  const router = useRouter();
  const confirm = useConfirm();
  const [watch, setWatch] = useState(initial);
  const [preview, setPreview] = useState<WatchPreview | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError(null);
    try {
      await fn();
    } catch (e) {
      // The service's message names the field and the bound — "minSamples 9
      // exceeds the 3-bucket window" — so it is shown rather than replaced.
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const save = () =>
    run(async () => {
      const saved = watchId
        ? await saveWatch(watchId, watch)
        : await createWatch(watch);
      router.push(`/platform/metrics/alerts/${encodeURIComponent(saved.id)}`);
      router.refresh();
    });

  const tryIt = () => run(async () => setPreview(await previewWatch(watch)));

  const remove = async () => {
    if (!watchId) return;
    const ok = await confirm({
      title: `Delete ${watch.name || "this watch"}?`,
      body: "Its evaluation history and every episode it recorded go with it. Nothing here can be undone.",
      confirmLabel: "Delete",
      danger: true,
    });
    if (!ok) return;
    await run(async () => {
      await deleteWatch(watchId);
      router.push("/platform/metrics/alerts");
      router.refresh();
    });
  };

  const setConditions = (next: WatchInput["conditions"]) =>
    setWatch({ ...watch, conditions: next });

  return (
    <div className="flex flex-col gap-5">
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label="Name">
          <input
            value={watch.name}
            aria-label="Name"
            onChange={(e) => setWatch({ ...watch, name: e.target.value })}
            className={`${INPUT} w-full`}
          />
        </Field>
        <Field label="Description">
          <input
            value={watch.description}
            aria-label="Description"
            onChange={(e) =>
              setWatch({ ...watch, description: e.target.value })
            }
            className={`${INPUT} w-full`}
          />
        </Field>
      </div>

      <label className="flex w-fit items-center gap-2 text-sm">
        <input
          type="checkbox"
          checked={watch.enabled}
          onChange={(e) => setWatch({ ...watch, enabled: e.target.checked })}
        />
        Enabled
      </label>

      <ConditionList
        combinator={watch.combinator}
        conditions={watch.conditions}
        onCombinator={(combinator) => setWatch({ ...watch, combinator })}
        onChange={setConditions}
      />

      <section>
        <h2 className="text-sm font-medium">Schedule</h2>
        <div className="mt-3">
          <ScheduleFields watch={watch} onChange={setWatch} />
        </div>
      </section>

      <section>
        <h2 className="text-sm font-medium">Then</h2>
        <div className="mt-3">
          <ActionList
            actions={watch.actions}
            onChange={(actions) => setWatch({ ...watch, actions })}
          />
        </div>
      </section>

      {error && (
        <p
          role="alert"
          className="rounded-lg bg-red-500/10 px-3 py-2 text-sm text-red-600 dark:text-red-400"
        >
          {error}
        </p>
      )}
      {preview && <PreviewPanel preview={preview} />}

      <div className="flex flex-wrap items-center gap-2">
        <button
          type="button"
          onClick={save}
          disabled={busy || watch.name.trim() === ""}
          className="rounded-lg bg-zinc-900 px-3 py-1.5 text-sm font-medium text-white hover:bg-zinc-700 disabled:opacity-50 dark:bg-zinc-100 dark:text-zinc-900 dark:hover:bg-zinc-300"
        >
          {watchId ? "Save" : "Create"}
        </button>
        <button
          type="button"
          onClick={tryIt}
          disabled={busy}
          className="rounded-lg border border-black/10 px-3 py-1.5 text-sm hover:bg-black/5 disabled:opacity-50 dark:border-white/10 dark:hover:bg-white/5"
        >
          Try it now
        </button>
        {watchId && (
          <button
            type="button"
            onClick={remove}
            disabled={busy}
            className="ml-auto rounded-lg border border-red-500/30 px-3 py-1.5 text-sm text-red-600 hover:bg-red-500/10 disabled:opacity-50 dark:text-red-400"
          >
            Delete
          </button>
        )}
      </div>
    </div>
  );
}
