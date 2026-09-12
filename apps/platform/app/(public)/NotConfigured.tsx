/**
 * Names the settings sign-in needs and this install does not have.
 *
 * Renders names only. A variable's name is safe to show to whoever asked; its
 * value is not.
 */
export default function NotConfigured({ missing }: { missing: string[] }) {
  return (
    <div className="w-full rounded-lg border border-amber-500/30 bg-amber-50 p-4 text-left dark:bg-amber-950/30">
      <p className="text-sm font-medium text-amber-900 dark:text-amber-200">
        Single sign-on is not configured
      </p>
      <p className="mt-1 text-xs text-amber-800 dark:text-amber-300/90">
        Nobody can sign in until an identity provider is configured. These
        settings are missing:
      </p>
      <ul className="mt-2 flex flex-col gap-0.5">
        {missing.map((name) => (
          <li
            key={name}
            className="font-mono text-xs text-amber-900 dark:text-amber-200"
          >
            {name}
          </li>
        ))}
      </ul>
    </div>
  );
}
