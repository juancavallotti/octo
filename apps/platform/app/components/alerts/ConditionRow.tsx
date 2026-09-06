"use client";

import { Trash2 } from "lucide-react";
import { Field, INPUT } from "@/app/components/admin/fields";
import {
  KIND_HINT,
  KIND_LABEL,
  METRICS,
  SOURCE_LABEL,
  defaultParams,
} from "./catalogue";
import { ConditionParams } from "./ConditionParams";
import type {
  AlertCondition,
  AlertConditionKind,
  AlertSource,
} from "@/app/model/alerts";

/**
 * One condition in a watch's set.
 *
 * Source, then metric, then how to judge it — in that order because each narrows
 * the next, and because it is the order somebody says it out loud: "the error
 * rate on traces, suddenly up".
 *
 * Changing the source or the kind resets what depends on it rather than carrying
 * parameters across. A spike's baseline means nothing to a threshold, and leaving
 * it in place would save a definition with a field the service refuses — which is
 * safe, but the refusal would name a parameter nobody can see.
 */
export function ConditionRow({
  condition,
  index,
  removable,
  onChange,
  onRemove,
}: {
  condition: AlertCondition;
  index: number;
  removable: boolean;
  onChange: (next: AlertCondition) => void;
  onRemove: () => void;
}) {
  const metrics = METRICS[condition.source];
  const choice = metrics.find((m) => m.metric === condition.metric);

  const setSource = (source: AlertSource) => {
    const first = METRICS[source][0];
    onChange({
      ...condition,
      source,
      // Pod-stat metrics are named by the operator, so switching to that source
      // leaves the field empty for them to fill rather than inventing a name.
      metric: first?.metric ?? "",
      aggregate:
        first?.aggregates[0] ?? (source === "pod_stats" ? "max" : "count"),
      scope: {},
    });
  };

  const setKind = (type: AlertConditionKind) =>
    onChange({ ...condition, type, params: defaultParams(type) });

  return (
    <li className="rounded-lg border border-black/10 p-3 dark:border-white/10">
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs font-medium text-zinc-500 dark:text-zinc-400">
          Condition {index + 1}
        </span>
        {removable && (
          <button
            type="button"
            onClick={onRemove}
            aria-label={`Remove condition ${index + 1}`}
            className="rounded p-1 text-zinc-500 hover:bg-black/5 hover:text-red-600 dark:hover:bg-white/5"
          >
            <Trash2 size={14} />
          </button>
        )}
      </div>

      <div className="mt-2 grid gap-3 sm:grid-cols-3">
        <Field label="Source">
          <select
            value={condition.source}
            aria-label={`Condition ${index + 1} source`}
            onChange={(e) => setSource(e.target.value as AlertSource)}
            className={`${INPUT} w-full`}
          >
            {(Object.keys(SOURCE_LABEL) as AlertSource[]).map((s) => (
              <option key={s} value={s}>
                {SOURCE_LABEL[s]}
              </option>
            ))}
          </select>
        </Field>

        <Field
          label="Metric"
          hint={
            condition.source === "pod_stats"
              ? "Any metric the runtime exports, e.g. go_memstats_heap_inuse_bytes"
              : undefined
          }
        >
          {condition.source === "pod_stats" ? (
            <input
              value={condition.metric}
              aria-label={`Condition ${index + 1} metric`}
              onChange={(e) =>
                onChange({ ...condition, metric: e.target.value })
              }
              className={`${INPUT} w-full font-mono`}
            />
          ) : (
            <select
              value={condition.metric}
              aria-label={`Condition ${index + 1} metric`}
              onChange={(e) => {
                const next = metrics.find((m) => m.metric === e.target.value);
                onChange({
                  ...condition,
                  metric: e.target.value,
                  aggregate: next?.aggregates[0],
                });
              }}
              className={`${INPUT} w-full`}
            >
              {metrics.map((m) => (
                <option key={m.metric} value={m.metric}>
                  {m.label}
                </option>
              ))}
            </select>
          )}
        </Field>

        <Field label="Aggregate">
          <select
            value={condition.aggregate ?? ""}
            aria-label={`Condition ${index + 1} aggregate`}
            onChange={(e) =>
              onChange({
                ...condition,
                aggregate: e.target.value as AlertCondition["aggregate"],
              })
            }
            className={`${INPUT} w-full`}
          >
            {(choice?.aggregates ?? ["max", "avg", "sum", "min"]).map((a) => (
              <option key={a} value={a}>
                {a}
              </option>
            ))}
          </select>
        </Field>
      </div>

      <div className="mt-3">
        <Field label="Judge it by" hint={KIND_HINT[condition.type]}>
          <select
            value={condition.type}
            aria-label={`Condition ${index + 1} kind`}
            onChange={(e) => setKind(e.target.value as AlertConditionKind)}
            className={`${INPUT} w-full sm:w-72`}
          >
            {(Object.keys(KIND_LABEL) as AlertConditionKind[]).map((k) => (
              <option key={k} value={k}>
                {KIND_LABEL[k]}
              </option>
            ))}
          </select>
        </Field>
      </div>

      <ConditionParams
        condition={condition}
        index={index}
        unit={choice?.unit}
        onChange={onChange}
      />
    </li>
  );
}
