import Image from "next/image";
import Link from "next/link";

/**
 * The Octo brand mark — logo + wordmark — used consistently in every top bar
 * across the signed-in platform. It links to the dashboard (`/platform`), so from
 * the file manager or the editor a click on the logo returns home. Pass
 * `href={null}` on pages where the logo should not navigate (e.g. the dashboard
 * itself or the public welcome page).
 */
export default function AppLogo({
  href = "/platform",
}: {
  /** Where the mark navigates; `null` renders it inert (no link). */
  href?: string | null;
}) {
  const mark = (
    <span className="flex items-center gap-2">
      {/* h-6 w-auto controls both axes so Tailwind's `img { height: auto }`
          reset doesn't trigger Next's aspect-ratio warning. */}
      <Image
        src="/octo-logo.png"
        alt="Octo logo"
        width={24}
        height={24}
        className="h-6 w-auto"
        priority
      />
      <span className="font-semibold tracking-tight">Octo</span>
    </span>
  );

  if (!href) return mark;

  return (
    <Link
      href={href}
      title="Octo — back to dashboard"
      className="rounded-md outline-none transition-opacity hover:opacity-80 focus-visible:ring-2 focus-visible:ring-sky-500"
    >
      {mark}
    </Link>
  );
}
