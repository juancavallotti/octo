"use client";

import { Plus } from "lucide-react";
import { INPUT } from "@/app/components/admin/fields";
import { ConditionRow } from "./ConditionRow";
import { newCondition } from "./catalogue";
import type { AlertCondition, WatchInput } from "@/app/model/alerts";
import type { WatchTarget } from "./target";

/** The service's own cap, mirrored so the button says so before a save is refused. */
const MAX_CONDITIONS = 10;

/**
 * The condition set and the combinator that joins it.
 *
 * "Alert when all/any of these are true" reads as the sentence somebody would
 * say, and the combinator sits inside it rather than in a settings block,
 * because it is the word that changes what the whole list means.
 */
export function ConditionList({
  combinator,
  conditions,
  target,
  onCombinator,
  onChange,
}: {
  combinator: WatchInput["combinator"];
  conditions: AlertCondition[];
  target: WatchTarget;
  onCombinator: (next: WatchInput["combinator"]) => void;
  onChange: (next: AlertCondition[]) => void;
}) {
  return (
    <div>
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <span>Alert when</span>
        <select
          value={combinator}
          aria-label="Combinator"
          onChange={(e) =>
            onCombinator(e.target.value as WatchInput["combinator"])
          }
          className={INPUT}
        >
          <option value="all">all</option>
          <option value="any">any</option>
        </select>
        <span>of these are true:</span>
      </div>

      <ul className="mt-3 flex flex-col gap-3">
        {conditions.map((condition, index) => (
          <ConditionRow
            key={condition.id}
            condition={condition}
            index={index}
            target={target}
            removable={conditions.length > 1}
            onChange={(next) =>
              onChange(conditions.map((c, i) => (i === index ? next : c)))
            }
            onRemove={() => onChange(conditions.filter((_, i) => i !== index))}
          />
        ))}
      </ul>

      <button
        type="button"
        disabled={conditions.length >= MAX_CONDITIONS}
        onClick={() => onChange([...conditions, newCondition()])}
        className="mt-3 flex items-center gap-1.5 rounded border border-black/10 px-2 py-1 text-xs hover:bg-black/5 disabled:opacity-50 dark:border-white/10 dark:hover:bg-white/5"
      >
        <Plus size={12} />
        Add a condition
      </button>
    </div>
  );
}
