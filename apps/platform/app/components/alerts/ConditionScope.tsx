"use client";

import { useEffect, useState } from "react";
import { Field, INPUT } from "@/app/components/admin/fields";
import { listStatsMetrics } from "@/app/model/stats";
import type { AlertCondition, AlertScope } from "@/app/model/alerts";
import type { WatchTarget } from "./target";

/**
 * What narrows a condition *within* the app the watch is already about.
 *
 * The app itself is not here any more — it is chosen once, at the top, and
 * written onto every condition. What is left is genuinely per condition and
 * genuinely per source: which log levels, which message, which runtime metric.
 *
 * A condition with nothing source-specific to ask renders nothing at all, rather
 * than a row of empty boxes that look like they want filling in.
 */
export function ConditionScope({
  condition,
  index,
  target,
  onChange,
}: {
  condition: AlertCondition;
  index: number;
  target: WatchTarget;
  onChange: (next: AlertCondition) => void;
}) {
  const scope = condition.scope ?? {};
  const set = (key: keyof AlertScope, value: unknown) =>
    onChange({
      ...condition,
      scope: { ...scope, [key]: value === "" ? undefined : value },
    });

  if (condition.source === "pod_stats") {
    return (
      <PodStatMetric
        condition={condition}
        index={index}
        target={target}
        onChange={onChange}
      />
    );
  }

  if (condition.source !== "logs") return null;

  return (
    <div className="mt-3 grid gap-3 sm:grid-cols-2">
      <Field label="Levels" hint="Comma separated. Empty means every level.">
        <input
          value={(scope.levels ?? []).join(", ")}
          aria-label={`Condition ${index + 1} levels`}
          onChange={(e) =>
            set(
              "levels",
              e.target.value
                .split(",")
                .map((s) => s.trim())
                .filter(Boolean),
            )
          }
          className={`${INPUT} w-full`}
          placeholder="error, fatal"
        />
      </Field>
      <Field
        label="Message contains"
        hint="Optional. Searching message text has no index behind it, so it is only allowed alongside an app."
      >
        <input
          value={scope.search ?? ""}
          aria-label={`Condition ${index + 1} search`}
          onChange={(e) => set("search", e.target.value)}
          className={`${INPUT} w-full`}
        />
      </Field>
    </div>
  );
}

/**
 * A runtime metric, completed from what this deployment is actually exporting.
 *
 * Pod-stat metric names come from the runtime's own registry rather than from any
 * catalogue this app could hold, so they are looked up per deployment. Typing a
 * name that nothing exports is the easy mistake here, and it produces a watch
 * that is permanently "no data" rather than an error.
 */
function PodStatMetric({
  condition,
  index,
  target,
  onChange,
}: {
  condition: AlertCondition;
  index: number;
  target: WatchTarget;
  onChange: (next: AlertCondition) => void;
}) {
  const deploymentId = condition.scope?.deploymentId || target.deploymentId;
  const [names, setNames] = useState<string[]>([]);
  const [problem, setProblem] = useState<string | null>(null);
  const listId = `metrics-${condition.id}`;

  useEffect(() => {
    if (!deploymentId) return;
    let stopped = false;
    const load = async () => {
      try {
        const page = await listStatsMetrics(deploymentId, {});
        if (!stopped) setNames(page.items.map((m) => m.name).sort());
      } catch (e) {
        if (!stopped) setProblem((e as Error).message);
      }
    };
    void load();
    return () => {
      stopped = true;
    };
  }, [deploymentId]);

  return (
    <div className="mt-3 flex flex-col gap-3">
      <Field
        label="Runtime metric"
        hint={
          names.length > 0
            ? `${names.length} exported by this deployment.`
            : (problem ??
              "Nothing is exporting metrics for this app yet — pod stats have to be turned on in the chart.")
        }
      >
        <input
          value={condition.metric}
          list={listId}
          aria-label={`Condition ${index + 1} metric`}
          onChange={(e) => onChange({ ...condition, metric: e.target.value })}
          className={`${INPUT} w-full font-mono`}
          placeholder="octo_flow_errors_total"
        />
      </Field>
      <datalist id={listId}>
        {names.map((name) => (
          <option key={name} value={name} />
        ))}
      </datalist>
      {!deploymentId && (
        <p className="text-xs text-amber-600 dark:text-amber-400">
          Pod stats are stored per deployment, so this condition needs one.
          Choose the app above.
        </p>
      )}
    </div>
  );
}
