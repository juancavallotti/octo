"use client";

import { Trash2 } from "lucide-react";
import { Field, INPUT } from "@/app/components/admin/fields";
import {
  KIND_HINT,
  KIND_LABEL,
  MEASURES,
  defaultParams,
  measureKey,
  measureOf,
} from "./catalogue";
import { ConditionParams } from "./ConditionParams";
import { ConditionScope } from "./ConditionScope";
import type { AlertCondition, AlertConditionKind } from "@/app/model/alerts";
import type { WatchTarget } from "./target";

/**
 * One condition: what to measure, and how to judge it.
 *
 * Source and metric are one picker rather than two. The second only ever meant
 * anything given the first, so choosing "Logs" and then discovering what logs can
 * measure was the wrong way round — the list of things you can measure is the
 * thing to read.
 *
 * The aggregate appears only when there is a choice to make. Most measures have
 * exactly one, and a dropdown with one option is a question with one answer.
 *
 * Changing the measure or the kind resets what depended on it. A spike's baseline
 * means nothing to a threshold, and carrying it across would save a definition
 * with a field nobody can see — refused by the service, naming a parameter that
 * is not on the form.
 */
export function ConditionRow({
  condition,
  index,
  target,
  step,
  removable,
  onChange,
  onRemove,
}: {
  condition: AlertCondition;
  index: number;
  target: WatchTarget;
  /** The bucket width this watch is measured in, which durations convert by. */
  step: number;
  removable: boolean;
  onChange: (next: AlertCondition) => void;
  onRemove: () => void;
}) {
  const measure = measureOf(condition);
  const aggregates = measure?.aggregates ?? [];

  const setMeasure = (key: string) => {
    const next = MEASURES.find((m) => measureKey(m.source, m.metric) === key);
    if (!next) return;
    onChange({
      ...condition,
      source: next.source,
      metric: next.metric,
      aggregate: next.aggregates[0],
      // The source-specific refinements do not survive: log levels mean nothing
      // to a pod-stat metric.
      scope: {},
    });
  };

  return (
    <li className="rounded-lg border border-black/10 p-3 dark:border-white/10">
      <div className="flex items-start gap-3">
        <div className="min-w-0 flex-1 grid gap-3 sm:grid-cols-2">
          <Field label={index === 0 ? "Measure" : "And measure"}>
            <select
              value={measureKey(condition.source, condition.metric)}
              aria-label={`Condition ${index + 1} measure`}
              onChange={(e) => setMeasure(e.target.value)}
              className={`${INPUT} w-full`}
            >
              {groups().map(([group, measures]) => (
                <optgroup key={group} label={group}>
                  {measures.map((m) => (
                    <option
                      key={measureKey(m.source, m.metric)}
                      value={measureKey(m.source, m.metric)}
                    >
                      {m.label}
                    </option>
                  ))}
                </optgroup>
              ))}
            </select>
          </Field>

          {aggregates.length > 1 && (
            <Field label="Measured as">
              <select
                value={condition.aggregate ?? aggregates[0]}
                aria-label={`Condition ${index + 1} aggregate`}
                onChange={(e) =>
                  onChange({
                    ...condition,
                    aggregate: e.target.value as AlertCondition["aggregate"],
                  })
                }
                className={`${INPUT} w-full`}
              >
                {aggregates.map((a) => (
                  <option key={a} value={a}>
                    {AGGREGATE_LABEL[a] ?? a}
                  </option>
                ))}
              </select>
            </Field>
          )}
        </div>

        {removable && (
          <button
            type="button"
            onClick={onRemove}
            aria-label={`Remove condition ${index + 1}`}
            className="mt-5 rounded p-1 text-zinc-500 hover:bg-black/5 hover:text-red-600 dark:hover:bg-white/5"
          >
            <Trash2 size={14} />
          </button>
        )}
      </div>

      <ConditionScope
        condition={condition}
        index={index}
        target={target}
        onChange={onChange}
      />

      <div className="mt-3">
        <Field label="Judge it by" hint={KIND_HINT[condition.type]}>
          <select
            value={condition.type}
            aria-label={`Condition ${index + 1} kind`}
            onChange={(e) =>
              onChange({
                ...condition,
                type: e.target.value as AlertConditionKind,
                params: defaultParams(e.target.value as AlertConditionKind),
              })
            }
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
        unit={measure?.unit}
        step={step}
        onChange={onChange}
      />
    </li>
  );
}

/** Plain words for the statistic, rather than the column name. */
const AGGREGATE_LABEL: Record<string, string> = {
  count: "a count",
  sum: "a total",
  avg: "the average",
  min: "the lowest",
  max: "the highest",
  p95: "the 95th percentile",
  ratio: "a rate",
};

/** The measures grouped by where they come from, in offer order. */
function groups(): [string, typeof MEASURES][] {
  const out = new Map<string, typeof MEASURES>();
  for (const measure of MEASURES) {
    out.set(measure.group, [...(out.get(measure.group) ?? []), measure]);
  }
  return [...out.entries()];
}
