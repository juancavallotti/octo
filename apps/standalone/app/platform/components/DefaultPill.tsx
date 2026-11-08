/**
 * Marks the environment every installation has, beside its name, so it reads apart
 * from the ones an administrator created. The platform's own switcher marks it the
 * same way.
 */
export default function DefaultPill() {
  return (
    <span className="shrink-0 rounded bg-black/[0.06] px-1 py-px text-[10px] font-medium uppercase tracking-wide text-zinc-500 dark:bg-white/10 dark:text-zinc-400">
      default
    </span>
  );
}
