"use client";

import { useState } from "react";
import { Field, INPUT } from "@/app/components/admin/fields";
import type { AlertAction } from "@/app/model/alerts";
import type { WatchTarget } from "./target";

/**
 * A comma-separated list of addresses.
 *
 * The raw text is local state and the parsed list is what leaves, which is the
 * whole point: parsing on every keystroke and rendering the result back means a
 * separator disappears the moment it is typed — `filter(Boolean)` drops the
 * empty segment after the comma, the value re-renders without it, and a second
 * address cannot be started.
 *
 * Re-seeded by remount rather than by an effect, so a fresh control replaces it
 * instead of an effect fighting whoever is typing.
 */
function AddressList({
  value,
  label,
  hint,
  placeholder,
  ariaLabel,
  onChange,
}: {
  value: string[];
  label: string;
  hint: string;
  placeholder?: string;
  ariaLabel: string;
  onChange: (addresses: string[]) => void;
}) {
  const [raw, setRaw] = useState(value.join(", "));
  return (
    <Field label={label} hint={hint}>
      <input
        value={raw}
        aria-label={ariaLabel}
        onChange={(e) => {
          setRaw(e.target.value);
          onChange(
            e.target.value
              .split(",")
              .map((s) => s.trim())
              .filter(Boolean),
          );
        }}
        className={`${INPUT} w-full`}
        placeholder={placeholder}
      />
    </Field>
  );
}

/** The two action bodies: one destination each. */

export function EmailFields({
  action,
  index,
  onChange,
}: {
  action: AlertAction;
  index: number;
  onChange: (next: AlertAction) => void;
}) {
  // Guarded: params come from stored JSON with no runtime shape check, and a
  // non-array here would throw on join and take the editor down with it.
  const to = Array.isArray(action.params?.to)
    ? (action.params.to as string[])
    : [];
  return (
    <div className="mt-3">
      <AddressList
        key={action.id}
        value={to}
        label="To"
        hint="Comma separated. Sent from the address configured in the platform's email settings."
        ariaLabel={`Action ${index + 1} recipients`}
        onChange={(addresses) =>
          onChange({ ...action, params: { ...action.params, to: addresses } })
        }
      />
    </div>
  );
}

/**
 * Where a message goes: which app receives it, and on what subject.
 *
 * The subject is completed from what is actually subscribed on the broker right
 * now. A receiver that is not running yet has no subscription, so the field stays
 * typeable — setting the watch up before the flow exists is legitimate. Left
 * blank, the destination is the app the watch is about.
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
  // Guarded for the same reason `to` is: a stored action whose reportTo is not
  // an array would throw on join and crash the editor rather than render.
  const reportTo = Array.isArray(params.reportTo)
    ? (params.reportTo as string[])
    : [];
  // Which app this action actually publishes to: the one chosen here, or the
  // watch's own app when that is left blank. Filtering on the raw field alone
  // would offer every subject on the installation whenever it is blank.
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
            The subscribed subjects, as a control rather than as a datalist: a
            datalist only appears once somebody types, so what this app is
            actually listening on would be invisible. This sets the field and
            holds no state of its own, so a subject that does not exist yet can
            still be typed — the receiving flow is often written after the watch.
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
      <AddressList
        key={action.id}
        value={reportTo}
        label="Who it should report to"
        hint="Optional, comma separated. For an app that investigates and writes back — it is told where to send its findings rather than deciding for itself."
        ariaLabel={`Action ${index + 1} report recipients`}
        placeholder="ada@example.com"
        onChange={(addresses) =>
          onChange({ ...action, params: { ...params, reportTo: addresses } })
        }
      />
    </div>
  );
}
