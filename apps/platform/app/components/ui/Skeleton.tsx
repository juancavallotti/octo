/**
 * Placeholders shaped like the content they stand in for, shown while it loads.
 * Shaped rather than generic so the page does not jump when the content lands,
 * and silent to assistive technology: the surrounding region says it is busy.
 */

const bar = "animate-pulse rounded bg-black/[0.06] dark:bg-white/[0.08]";

/** One line of text, `width` wide (any CSS width). */
export function SkeletonLine({
  width = "100%",
  className = "",
}: {
  width?: string;
  className?: string;
}) {
  return <span aria-hidden className={`block h-3 ${bar} ${className}`} style={{ width }} />;
}

/** `count` list rows: an icon and a line, the widths varied so it reads as a list. */
export function SkeletonRows({ count = 6 }: { count?: number }) {
  const widths = ["70%", "55%", "80%", "45%", "65%", "50%"];
  return (
    <ul aria-hidden className="space-y-3 px-3 py-2">
      {Array.from({ length: count }, (_, i) => (
        <li key={i} className="flex items-center gap-2">
          <span className={`h-4 w-4 shrink-0 ${bar}`} />
          <SkeletonLine width={widths[i % widths.length]} />
        </li>
      ))}
    </ul>
  );
}

/** A titled card with a few lines, for a section whose content is on its way. */
export function SkeletonCard({ title, lines = 3 }: { title: string; lines?: number }) {
  return (
    <section className="rounded-lg border border-black/10 p-3 dark:border-white/10">
      <h3 className="mb-3 text-xs font-semibold uppercase tracking-wide text-zinc-400">
        {title}
      </h3>
      <div aria-hidden className="space-y-2.5">
        {Array.from({ length: lines }, (_, i) => (
          <SkeletonLine key={i} width={i === lines - 1 ? "60%" : "100%"} />
        ))}
      </div>
    </section>
  );
}
