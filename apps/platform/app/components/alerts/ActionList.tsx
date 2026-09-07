"use client";

import { Plus, Trash2 } from "lucide-react";
import { Field, INPUT } from "@/app/components/admin/fields";
import { newId } from "./catalogue";
import { EmailFields, TopicFields } from "./ActionFields";
import { useTopicDestinations } from "./useTopicDestinations";
import type { AlertAction, AlertActionKind } from "@/app/model/alerts";
import type { WatchTarget } from "./target";

/**
 * Who hears about it.
 *
 * Two kinds, which are the two that were asked for: a message onto a
 * deployment's own topic, where a flow picks it up and can act on it, and an
 * email.
 *
 * A watch may have none. That is a real thing to want while tuning one — it still
 * records every check and opens incidents — so the section says so rather than
 * refusing, and rather than being pre-filled with something that looks configured
 * and reaches nobody.
 */
const ACTION_LABEL: Record<AlertActionKind, string> = {
  topic: "Send it to an app",
  email: "Send an email",
};

export function ActionList({
  actions,
  target,
  onChange,
}: {
  actions: AlertAction[];
  target: WatchTarget;
  onChange: (next: AlertAction[]) => void;
}) {
  const { destinations } = useTopicDestinations();

  return (
    <div className="flex flex-col gap-2">
      <ul className="flex flex-col gap-2">
        {actions.map((action, index) => (
          <li
            key={action.id}
            className="rounded-lg border border-black/10 p-3 dark:border-white/10"
          >
            <div className="flex items-start gap-3">
              <div className="min-w-0 flex-1">
                <Field label={`Action ${index + 1}`}>
                  <select
                    value={action.type}
                    aria-label={`Action ${index + 1} type`}
                    onChange={(e) =>
                      // Nothing carries across: a recipient list means nothing to
                      // a topic, and the service would refuse it.
                      onChange(
                        replace(actions, index, {
                          ...action,
                          type: e.target.value as AlertActionKind,
                          params: {},
                        }),
                      )
                    }
                    className={`${INPUT} w-full sm:w-72`}
                  >
                    {(Object.keys(ACTION_LABEL) as AlertActionKind[]).map(
                      (k) => (
                        <option key={k} value={k}>
                          {ACTION_LABEL[k]}
                        </option>
                      ),
                    )}
                  </select>
                </Field>
              </div>
              <button
                type="button"
                onClick={() => onChange(actions.filter((_, i) => i !== index))}
                aria-label={`Remove action ${index + 1}`}
                className="mt-5 rounded p-1 text-zinc-500 hover:bg-black/5 hover:text-red-600 dark:hover:bg-white/5"
              >
                <Trash2 size={14} />
              </button>
            </div>

            {action.type === "email" ? (
              <EmailFields
                action={action}
                index={index}
                onChange={(next) => onChange(replace(actions, index, next))}
              />
            ) : (
              <TopicFields
                action={action}
                index={index}
                target={target}
                destinations={destinations}
                onChange={(next) => onChange(replace(actions, index, next))}
              />
            )}
          </li>
        ))}
      </ul>

      <button
        type="button"
        onClick={() =>
          onChange([...actions, { id: newId("a"), type: "email", params: {} }])
        }
        className="flex w-fit items-center gap-1.5 rounded border border-black/10 px-2 py-1 text-xs hover:bg-black/5 dark:border-white/10 dark:hover:bg-white/5"
      >
        <Plus size={12} />
        Add somewhere to send it
      </button>

      {actions.length === 0 && (
        <p className="text-xs text-amber-600 dark:text-amber-400">
          This watch will record every check and open incidents, but it will not
          tell anybody when it fires.
        </p>
      )}
    </div>
  );
}

function replace(
  actions: AlertAction[],
  index: number,
  next: AlertAction,
): AlertAction[] {
  return actions.map((a, i) => (i === index ? next : a));
}
