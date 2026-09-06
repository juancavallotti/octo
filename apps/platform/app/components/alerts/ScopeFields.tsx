"use client";

import { Field, INPUT } from "@/app/components/admin/fields";
import type { AlertCondition, AlertScope } from "@/app/model/alerts";

/**
 * What a condition is narrowed to.
 *
 * The axes differ per source, so only the ones that apply are shown: an
 * integration id means nothing to a log query, and a pod-stat condition needs a
 * deployment because the stats are stored under one.
 *
 * An empty field is no constraint, which is why every one of them is optional and
 * none has a placeholder that looks like a default.
 */
export function ScopeFields({
  condition,
  index,
  onChange,
}: {
  condition: AlertCondition;
  index: number;
  onChange: (next: AlertCondition) => void;
}) {
  const scope = condition.scope ?? {};
  const set = (key: keyof AlertScope, value: string) =>
    onChange({
      ...condition,
      scope: { ...scope, [key]: value === "" ? undefined : value },
    });

  return (
    <>
      <Field
        label="Deployment"
        hint={
          condition.source === "pod_stats"
            ? "Required: pod stats are stored per deployment."
            : undefined
        }
      >
        <input
          value={scope.deploymentId ?? ""}
          aria-label={`Condition ${index + 1} deployment`}
          onChange={(e) => set("deploymentId", e.target.value)}
          className={`${INPUT} w-full font-mono`}
        />
      </Field>

      {condition.source !== "pod_stats" && (
        <Field label="App name">
          <input
            value={scope.appName ?? ""}
            aria-label={`Condition ${index + 1} app`}
            onChange={(e) => set("appName", e.target.value)}
            className={`${INPUT} w-full`}
          />
        </Field>
      )}

      {condition.source === "traces" && (
        <Field
          label="Integration"
          hint="Survives a rollout, where a deployment id does not."
        >
          <input
            value={scope.integrationId ?? ""}
            aria-label={`Condition ${index + 1} integration`}
            onChange={(e) => set("integrationId", e.target.value)}
            className={`${INPUT} w-full font-mono`}
          />
        </Field>
      )}

      {condition.source === "logs" && (
        <>
          <Field
            label="Levels"
            hint="Comma separated. Empty means every level."
          >
            <input
              value={(scope.levels ?? []).join(", ")}
              aria-label={`Condition ${index + 1} levels`}
              onChange={(e) =>
                onChange({
                  ...condition,
                  scope: { ...scope, levels: splitLevels(e.target.value) },
                })
              }
              className={`${INPUT} w-full`}
            />
          </Field>
          <Field
            label="Message contains"
            hint="Needs a deployment or app alongside it — an unbounded search reads every log line on the install, once a minute."
          >
            <input
              value={scope.search ?? ""}
              aria-label={`Condition ${index + 1} search`}
              onChange={(e) => set("search", e.target.value)}
              className={`${INPUT} w-full`}
            />
          </Field>
        </>
      )}
    </>
  );
}

/** Split a comma-separated list, dropping the empties a trailing comma leaves. */
function splitLevels(raw: string): string[] | undefined {
  const levels = raw
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);
  return levels.length > 0 ? levels : undefined;
}
