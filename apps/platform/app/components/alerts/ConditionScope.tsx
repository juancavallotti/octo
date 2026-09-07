"use client";

import { Field, INPUT } from "@/app/components/admin/fields";
import { MetricPicker } from "./MetricPicker";
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
      <div className="mt-3">
        <MetricPicker
          deploymentId={scope.deploymentId || target.deploymentId}
          value={condition.metric}
          onChange={(metric) => onChange({ ...condition, metric })}
        />
      </div>
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
