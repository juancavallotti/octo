"use client";

/**
 * A small one-of-N control, used wherever the suite format offers a choice that is
 * genuinely exclusive — a case's input, an expectation's outcome, the Form/YAML view.
 *
 * Exclusive is the point. dolphin rejects an expectation that states two outcomes, so
 * offering them as checkboxes would be offering a file that will not load; a control the
 * user cannot get wrong beats an error message explaining that they did.
 */
export default function Segmented<T extends string>({
  options,
  value,
  onChange,
  disabled = [],
  disabledReason,
  ariaLabel,
}: {
  options: { key: T; label: string }[];
  value: T;
  onChange: (next: T) => void;
  /** Choices that are not available right now (e.g. a shared input with none declared). */
  disabled?: T[];
  /** Why, as a tooltip on the disabled choices. */
  disabledReason?: string;
  ariaLabel?: string;
}) {
  return (
    <div className="flex gap-0.5" role="group" aria-label={ariaLabel}>
      {options.map((option) => {
        const off = disabled.includes(option.key);
        const on = option.key === value;
        return (
          <button
            key={option.key}
            type="button"
            aria-pressed={on}
            disabled={off}
            title={off ? disabledReason : undefined}
            onClick={() => onChange(option.key)}
            className={`rounded-md px-2 py-0.5 text-xs transition-colors ${
              on
                ? "bg-sky-600 text-white"
                : "text-zinc-500 hover:bg-black/[0.05] disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-transparent dark:hover:bg-white/10"
            }`}
          >
            {option.label}
          </button>
        );
      })}
    </div>
  );
}
