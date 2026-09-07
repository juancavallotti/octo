import type { ReactNode } from "react";

/**
 * One numbered step of the editor.
 *
 * Numbered because the form is a sequence of decisions rather than a set of
 * fields: each one narrows the next, and the app at the top decides what every
 * condition below it can even measure. Unnumbered sections read as things you
 * could fill in any order, which this is not.
 */
export function Section({
  title,
  step,
  children,
}: {
  title: string;
  step: number;
  children: ReactNode;
}) {
  return (
    <section aria-label={title}>
      <h2 className="flex items-center gap-2 text-sm font-medium">
        <span
          aria-hidden
          className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-black/5 text-[11px] font-semibold text-zinc-500 dark:bg-white/10 dark:text-zinc-400"
        >
          {step}
        </span>
        {title}
      </h2>
      <div className="mt-3">{children}</div>
    </section>
  );
}
