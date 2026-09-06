"use client";

import { Plus, Trash2 } from "lucide-react";
import { Field, INPUT } from "@/app/components/admin/fields";
import { newId } from "./catalogue";
import type { AlertAction, AlertActionKind } from "@/app/model/alerts";

/**
 * What a watch does when it fires.
 *
 * `log` is first and is what a new watch starts with, because it needs no
 * configuration at all: a watch that fires and tells nobody is the easiest
 * mistake to make here, and this makes the default at least record itself
 * somewhere an operator will see.
 */
const ACTION_LABEL: Record<AlertActionKind, string> = {
  log: "Write a log line",
  email: "Send an email",
  topic: "Publish to a deployment's topic",
};

export function ActionList({
  actions,
  onChange,
}: {
  actions: AlertAction[];
  onChange: (next: AlertAction[]) => void;
}) {
  const replace = (index: number, next: AlertAction) =>
    onChange(actions.map((a, i) => (i === index ? next : a)));

  return (
    <div className="flex flex-col gap-2">
      <ul className="flex flex-col gap-2">
        {actions.map((action, index) => (
          <ActionRow
            key={action.id}
            action={action}
            index={index}
            onChange={(next) => replace(index, next)}
            onRemove={() => onChange(actions.filter((_, i) => i !== index))}
          />
        ))}
      </ul>
      <button
        type="button"
        onClick={() =>
          onChange([...actions, { id: newId("a"), type: "log", params: {} }])
        }
        className="flex w-fit items-center gap-1.5 rounded border border-black/10 px-2 py-1 text-xs hover:bg-black/5 dark:border-white/10 dark:hover:bg-white/5"
      >
        <Plus size={12} />
        Add an action
      </button>
      {actions.length === 0 && (
        <p className="text-xs text-amber-600 dark:text-amber-400">
          With no actions this watch will still record every evaluation, but it
          will not tell anybody when it fires.
        </p>
      )}
    </div>
  );
}

function ActionRow({
  action,
  index,
  onChange,
  onRemove,
}: {
  action: AlertAction;
  index: number;
  onChange: (next: AlertAction) => void;
  onRemove: () => void;
}) {
  const params = action.params ?? {};
  const set = (key: string, value: unknown) =>
    onChange({ ...action, params: { ...params, [key]: value } });

  return (
    <li className="rounded-lg border border-black/10 p-3 dark:border-white/10">
      <div className="flex items-start gap-3">
        <div className="flex-1">
          <Field label={`Action ${index + 1}`}>
            <select
              value={action.type}
              aria-label={`Action ${index + 1} type`}
              onChange={(e) =>
                // The parameters do not carry across: a recipient list means
                // nothing to a topic, and the service would refuse it.
                onChange({
                  ...action,
                  type: e.target.value as AlertActionKind,
                  params: {},
                })
              }
              className={`${INPUT} w-full sm:w-72`}
            >
              {(Object.keys(ACTION_LABEL) as AlertActionKind[]).map((k) => (
                <option key={k} value={k}>
                  {ACTION_LABEL[k]}
                </option>
              ))}
            </select>
          </Field>
        </div>
        <button
          type="button"
          onClick={onRemove}
          aria-label={`Remove action ${index + 1}`}
          className="mt-5 rounded p-1 text-zinc-500 hover:bg-black/5 hover:text-red-600 dark:hover:bg-white/5"
        >
          <Trash2 size={14} />
        </button>
      </div>

      {action.type === "email" && (
        <div className="mt-3">
          <Field
            label="To"
            hint="Comma separated. Sent through the platform's configured email settings."
          >
            <input
              value={((params.to as string[]) ?? []).join(", ")}
              aria-label={`Action ${index + 1} recipients`}
              onChange={(e) =>
                set(
                  "to",
                  e.target.value
                    .split(",")
                    .map((s) => s.trim())
                    .filter(Boolean),
                )
              }
              className={`${INPUT} w-full`}
            />
          </Field>
        </div>
      )}

      {action.type === "topic" && (
        <div className="mt-3 grid gap-3 sm:grid-cols-2">
          <Field
            label="Deployment"
            hint="The app that acts on the alert — usually not the one the alert is about."
          >
            <input
              value={String(params.deploymentId ?? "")}
              aria-label={`Action ${index + 1} deployment`}
              onChange={(e) => set("deploymentId", e.target.value)}
              className={`${INPUT} w-full font-mono`}
            />
          </Field>
          <Field
            label="Subject"
            hint="What that app's events source subscribes to. No wildcards."
          >
            <input
              value={String(params.subject ?? "")}
              aria-label={`Action ${index + 1} subject`}
              onChange={(e) => set("subject", e.target.value)}
              className={`${INPUT} w-full font-mono`}
            />
          </Field>
        </div>
      )}
    </li>
  );
}
