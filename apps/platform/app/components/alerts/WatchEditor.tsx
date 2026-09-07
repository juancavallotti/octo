"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { useConfirm } from "@/app/components/ConfirmDialog";
import {
  createWatch,
  deleteWatch,
  previewWatch,
  saveWatch,
  type WatchInput,
  type WatchPreview,
} from "@/app/model/alerts";
import { ActionList } from "./ActionList";
import { ConditionList } from "./ConditionList";
import { Deduplication } from "./Deduplication";
import { PreviewPanel } from "./PreviewPanel";
import { Schedule } from "./Schedule";
import { Section } from "./Section";
import { WatchIdentity } from "./WatchIdentity";
import { WatchTargetPicker } from "./WatchTargetPicker";
import { STEP_SECONDS } from "./catalogue";
import {
  applyTarget,
  fillTarget,
  targetOf,
  targetsAgree,
  type WatchTarget,
} from "./target";

/**
 * Writing a watch, in the order the decisions are actually made: what you are
 * watching, what to call it, how often to look, what would be wrong, who to
 * tell, and how not to be told twice.
 *
 * The app comes first because it is the only answer everything else depends on —
 * every condition is measured over it, and the earlier form asked for it once per
 * condition, as a uuid, near the bottom.
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
  const [target, setTarget] = useState<WatchTarget>(() =>
    targetOf(initial.conditions),
  );
  const [preview, setPreview] = useState<WatchPreview | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  // A watch written over the API may scope its conditions differently from one
  // another. There is no single app that describes that, so the editor says so
  // and leaves them alone until somebody actually picks one.
  const [mixed] = useState(() => !targetsAgree(initial.conditions));

  const retarget = (next: WatchTarget) => {
    setTarget(next);
    setWatch((w) => ({ ...w, conditions: applyTarget(w.conditions, next) }));
  };

  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError(null);
    try {
      await fn();
    } catch (e) {
      // The service's message names the field and the bound, so it is shown
      // rather than replaced with something generic.
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  // The bucket width is not on the form; every save writes the one the editor
  // measures in, so a window in minutes means minutes.
  const submitted = (): WatchInput => ({ ...watch, stepSeconds: STEP_SECONDS });

  const save = () =>
    run(async () => {
      const saved = watchId
        ? await saveWatch(watchId, submitted())
        : await createWatch(submitted());
      router.push(`/platform/metrics/alerts/${encodeURIComponent(saved.id)}`);
      router.refresh();
    });

  const tryIt = () =>
    run(async () => setPreview(await previewWatch(submitted())));

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

  return (
    <div className="flex flex-col gap-6">
      <Section title="What are you watching?" step={1}>
        <WatchTargetPicker target={target} onChange={retarget} />
        {mixed && (
          <p className="mt-2 text-xs text-amber-600 dark:text-amber-400">
            This watch&rsquo;s conditions were set up to look at different
            things. Choosing an app here will point all of them at it.
          </p>
        )}
      </Section>

      <Section title="What is it called?" step={2}>
        <WatchIdentity watch={watch} onChange={setWatch} />
      </Section>

      <Section title="How often should we look?" step={3}>
        <Schedule watch={watch} onChange={setWatch} />
      </Section>

      <Section title="What would be wrong?" step={4}>
        <ConditionList
          combinator={watch.combinator}
          conditions={watch.conditions}
          target={target}
          onCombinator={(combinator) => setWatch({ ...watch, combinator })}
          // Filled rather than replaced: a condition just added, or one whose
          // measure changed, has no scope yet and would otherwise be measured
          // over the whole installation.
          onChange={(conditions) =>
            setWatch({ ...watch, conditions: fillTarget(conditions, target) })
          }
        />
      </Section>

      <Section title="Who should hear about it?" step={5}>
        <ActionList
          actions={watch.actions}
          target={target}
          onChange={(actions) => setWatch({ ...watch, actions })}
        />
      </Section>

      <Section title="How often should we say so?" step={6}>
        <Deduplication watch={watch} onChange={setWatch} />
      </Section>

      {error && (
        <p
          role="alert"
          className="rounded-lg bg-red-500/10 px-3 py-2 text-sm text-red-600 dark:text-red-400"
        >
          {error}
        </p>
      )}
      {preview && <PreviewPanel preview={preview} />}

      <div className="flex flex-wrap items-center gap-2 border-t border-black/10 pt-4 dark:border-white/10">
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
