"use client";

import { AlertTriangle } from "lucide-react";
import { SecondaryButton } from "./fields";

/**
 * Whether the alert troubleshooter may change this installation, or may only
 * look at it and say what it found.
 *
 * Its own control rather than an icon toggle beside tracing, because the two are
 * not the same size of decision. Tracing costs throughput and a pod restart. This
 * one decides whether an alert firing at four in the morning can end in a rollout
 * nobody watched — so it gets a sentence explaining what it means, and turning it
 * on takes a deliberate click on a button that says so.
 *
 * The state is rendered as prose rather than as a switch for the same reason. A
 * switch invites flipping; a line that says what is currently true, next to a
 * button that says what would change, invites reading first.
 */
export default function AgentAutoFix({
  autoFix,
  disabled,
  onToggle,
}: {
  autoFix: boolean;
  disabled: boolean;
  onToggle: () => void;
}) {
  return (
    <div className="flex flex-wrap items-start justify-between gap-3">
      <div className="min-w-0">
        <p className="text-sm font-medium">
          {autoFix
            ? "Dr. Octo may fix what an alert finds"
            : "Dr. Octo investigates alerts, but changes nothing"}
        </p>
        <p className="mt-0.5 max-w-prose text-xs text-zinc-500 dark:text-zinc-400">
          {autoFix ? (
            <>
              An alert can end in a change nobody watched — scaling, tracing, or
              rolling out a corrected definition. He still reports what he did,
              and he still refuses to act on a fault he cannot explain.
            </>
          ) : (
            <>
              He triages every alert and emails what he found, including what he
              would change. Turning this on lets him carry that out.
            </>
          )}
        </p>
      </div>
      <SecondaryButton onClick={onToggle} disabled={disabled}>
        {autoFix ? "Restrict to reporting" : "Let him fix things"}
      </SecondaryButton>
      {autoFix && (
        <p className="flex w-full items-center gap-1.5 text-xs text-amber-600 dark:text-amber-400">
          <AlertTriangle size={12} className="shrink-0" />
          Changes are made through the agent&rsquo;s operator, which holds the
          unrestricted API.
        </p>
      )}
    </div>
  );
}
