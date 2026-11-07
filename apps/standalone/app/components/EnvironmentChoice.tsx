"use client";

/**
 * The environment the installation lists, and a way to pick another, beside the
 * profile's name. Only a choice when there is one; a single environment is said, not
 * offered. Picking saves it on the profile, as the Platform window's picker does.
 */
export default function EnvironmentChoice({
  current,
  environments,
  disabled,
  onPick,
}: {
  current: string;
  environments: string[] | null;
  disabled: boolean;
  onPick(environment: string): void;
}) {
  if (!environments || environments.length < 2) {
    return current !== "octo" ? <> · {current}</> : null;
  }
  return (
    <>
      {" · "}
      <select
        aria-label="Environment"
        value={current}
        disabled={disabled}
        onChange={(e) => onPick(e.target.value)}
        // The heading sits inside the picker, which reads keys for its search and
        // closes on clicks it does not own; this control's are its own.
        onMouseDown={(e) => e.stopPropagation()}
        onKeyDown={(e) => e.stopPropagation()}
        className="rounded border border-black/10 bg-transparent px-1 py-px text-xs text-zinc-700 disabled:opacity-50 dark:border-white/15 dark:text-zinc-200"
      >
        {environments.map((e) => (
          <option key={e} value={e}>
            {e}
          </option>
        ))}
      </select>
    </>
  );
}
