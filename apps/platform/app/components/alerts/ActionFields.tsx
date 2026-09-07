"use client";

import { Field, INPUT } from "@/app/components/admin/fields";
import type { AlertAction } from "@/app/model/alerts";
import type { WatchTarget } from "./target";

/**
 * The two action bodies. Split from the list because the list is about adding
 * and removing, and these are about one destination each.
 */

export function EmailFields({
  action,
  index,
  onChange,
}: {
  action: AlertAction;
  index: number;
  onChange: (next: AlertAction) => void;
}) {
  const to = (action.params?.to as string[]) ?? [];
  return (
    <div className="mt-3">
      <Field
        label="To"
        hint="Comma separated. Sent from the address configured in the platform's email settings."
      >
        <input
          value={to.join(", ")}
          aria-label={`Action ${index + 1} recipients`}
          onChange={(e) =>
            onChange({
              ...action,
              params: {
                ...action.params,
                to: e.target.value
                  .split(",")
                  .map((s) => s.trim())
                  .filter(Boolean),
              },
            })
          }
          className={`${INPUT} w-full`}
        />
      </Field>
    </div>
  );
}

/**
 * Where a message goes: which app receives it, and on what subject.
 *
 * The subject is completed from what is actually subscribed on the broker right
 * now, which is the difference between picking a destination and guessing one. A
 * receiver that is not running yet has no subscription, so the field stays
 * typeable — setting the watch up before the flow exists is legitimate.
 *
 * The default deployment is the app the watch is about, because sending an alert
 * back to the thing it is about is the common case; anything else is typed or
 * picked from the live list.
 */
export function TopicFields({
  action,
  index,
  target,
  destinations,
  onChange,
}: {
  action: AlertAction;
  index: number;
  target: WatchTarget;
  destinations: {
    deploymentId: string;
    subject: string;
    subscribers: number;
  }[];
  onChange: (next: AlertAction) => void;
}) {
  const params = action.params ?? {};
  const deploymentId = String(params.deploymentId ?? "");
  const listId = `topics-${action.id}`;
  const reportTo = (params.reportTo as string[]) ?? [];
  // Which app this action actually publishes to: the one chosen here, or the
  // watch's own app when that is left blank. Filtering on the raw field alone
  // showed every subject on the installation whenever it was blank, and then
  // counted them in the hint.
  const receiving = deploymentId || target.deploymentId || "";
  const forThisApp = destinations.filter(
    (d) => !receiving || d.deploymentId === receiving,
  );
  const subjects = [...new Set(forThisApp.map((d) => d.subject))].sort();

  const set = (key: string, value: string) =>
    onChange({ ...action, params: { ...params, [key]: value } });

  return (
    <div className="mt-3 grid gap-3 sm:grid-cols-2">
      <Field
        label="Which app receives it"
        hint={
          target.deploymentId && !deploymentId
            ? "Leave blank to send it back to the app this watch is about."
            : undefined
        }
      >
        <select
          value={deploymentId}
          aria-label={`Action ${index + 1} destination`}
          onChange={(e) => set("deploymentId", e.target.value)}
          className={`${INPUT} w-full`}
        >
          <option value="">
            {target.appName ? `${target.appName} (this app)` : "Choose…"}
          </option>
          {[...new Set(destinations.map((d) => d.deploymentId))]
            .filter((id) => id !== target.deploymentId)
            .map((id) => (
              <option key={id} value={id}>
                {id}
              </option>
            ))}
        </select>
      </Field>

      <Field
        label="On subject"
        hint={
          subjects.length > 0
            ? "Pick one this app already listens on, or type a new one."
            : "Nothing is subscribed to a topic on this app yet. The flow that receives it uses an events source with this subject."
        }
      >
        <div className="flex gap-2">
          <input
            value={String(params.subject ?? "")}
            list={listId}
            aria-label={`Action ${index + 1} subject`}
            onChange={(e) => set("subject", e.target.value)}
            className={`${INPUT} w-full font-mono`}
            placeholder="alerts"
          />
          {/*
            The subscribed subjects, as a control rather than as a datalist.
            A datalist only appears once somebody types, so the one thing worth
            knowing here — what this app is actually listening on — was invisible
            to anyone who did not already know it. This sets the field and holds
            no state of its own, so a subject that does not exist yet can still
            be typed: the receiving flow is often written after the watch.
          */}
          {subjects.length > 0 && (
            <select
              value=""
              aria-label={`Action ${index + 1} subscribed subjects`}
              onChange={(e) => {
                if (e.target.value) set("subject", e.target.value);
              }}
              className={`${INPUT} shrink-0`}
            >
              <option value="">Subscribed…</option>
              {subjects.map((subject) => (
                <option key={subject} value={subject}>
                  {subject}
                </option>
              ))}
            </select>
          )}
        </div>
      </Field>

      <datalist id={listId}>
        {subjects.map((subject) => (
          <option key={subject} value={subject} />
        ))}
      </datalist>

      {/*
        Carried on the alert rather than configured inside the receiving app, so
        that whoever edits the watch can see who hears about it. It is optional
        because a flow that only records or reacts needs nobody's address.
      */}
      <Field
        label="Who it should report to"
        hint="Optional, comma separated. For an app that investigates and writes back — it is told where to send its findings rather than deciding for itself."
      >
        <input
          value={reportTo.join(", ")}
          aria-label={`Action ${index + 1} report recipients`}
          onChange={(e) =>
            onChange({
              ...action,
              params: {
                ...params,
                reportTo: e.target.value
                  .split(",")
                  .map((s) => s.trim())
                  .filter(Boolean),
              },
            })
          }
          className={`${INPUT} w-full`}
          placeholder="ada@example.com"
        />
      </Field>
    </div>
  );
}
