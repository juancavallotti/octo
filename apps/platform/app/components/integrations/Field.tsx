/** A labelled group of fields; the unit the deploy modal's sections plug into. */
export default function Field({
  label,
  children,
  hint,
}: {
  label: string;
  children: React.ReactNode;
  hint?: string;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-xs font-semibold uppercase tracking-wide text-zinc-400">
        {label}
      </span>
      {children}
      {hint && <span className="text-xs text-zinc-400">{hint}</span>}
    </div>
  );
}
