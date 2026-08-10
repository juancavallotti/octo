import type React from "react";

/**
 * The two layout atoms the detail pane is built from: a titled card, and a
 * label/value line inside one.
 *
 * Neither knows what an integration is — a Section takes a title and children, a
 * Row takes two nodes — which is why they sit apart from the pane that fills
 * them.
 */

/** A labelled section card; the unit the detail grid is composed of. `className`
 * lets a cell span columns in the grid. */
export function Section({
  title,
  className,
  children,
}: {
  title: string;
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <section
      className={`rounded-lg border border-black/10 p-3 dark:border-white/10 ${className ?? ""}`}
    >
      <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-zinc-400">
        {title}
      </h3>
      {children}
    </section>
  );
}

export function Row({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-3 py-0.5 text-sm">
      <span className="text-zinc-500">{label}</span>
      <span className="min-w-0 truncate text-right font-medium">{value}</span>
    </div>
  );
}
