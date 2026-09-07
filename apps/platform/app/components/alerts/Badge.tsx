/**
 * The small labelled pills alerting renders everywhere: a phase, a status, a
 * severity.
 *
 * One component rather than three, because the difference between them is which
 * class map they look up and nothing else — and three copies of the same markup
 * is how they drift apart.
 */
export function Badge({
  label,
  className,
  title,
}: {
  label: string;
  className: string;
  title?: string;
}) {
  return (
    <span
      title={title}
      className={`inline-flex shrink-0 items-center rounded px-1.5 py-0.5 text-[11px] font-medium ${className}`}
    >
      {label}
    </span>
  );
}
