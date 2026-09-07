"use client";

/**
 * Whether Dr. Octo may act on what an alert tells him, or only report it.
 *
 * A checkbox rather than a button, because it is a standing permission and not
 * an action: it describes how this installation is configured, and a button
 * ("Let him fix things") read as something that would happen when clicked. It
 * also sits with the other deployment settings rather than in a box of its own —
 * it changes the same pods the buttons above it do.
 *
 * The label says what is allowed and the line beneath says what happens when it
 * is not, because "off" here is not "nothing" — it is still a full triage and a
 * report. Somebody deciding this needs to know they lose the fixing, not the
 * investigating.
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
    <label className="flex cursor-pointer items-start gap-2.5">
      <input
        type="checkbox"
        checked={autoFix}
        disabled={disabled}
        onChange={onToggle}
        className="mt-0.5 h-4 w-4 shrink-0 cursor-pointer accent-blue-600 disabled:cursor-not-allowed disabled:opacity-50"
      />
      <span className="min-w-0">
        <span className="block text-sm font-medium">
          Allow Dr. Octo to troubleshoot applications
        </span>
        <span className="mt-0.5 block text-xs text-zinc-500 dark:text-zinc-400">
          When an alert fires he triages it either way and emails what he found.
          Ticked, he may also act on it — scaling, turning tracing on, or
          rolling out a corrected definition — and reports what he did.
          Unticked, you get the triage and nothing changes.
        </span>
      </span>
    </label>
  );
}
