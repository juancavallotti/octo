import Image from "next/image";
import Link from "next/link";
import { ArrowLeft } from "lucide-react";

export const metadata = {
  title: "Not found — Octo",
};

/**
 * Branded 404 for any unmatched route. Rendered inside the root layout, so it
 * stands alone (no app header — the user may be signed out). It points home to
 * the public welcome page, which then routes signed-in users on to the dashboard.
 */
export default function NotFound() {
  return (
    <main className="flex h-full flex-1 items-center justify-center p-6">
      <div className="flex w-full max-w-md flex-col items-center gap-6 text-center">
        <Image
          src="/octo-logo.png"
          alt="Octo logo"
          width={72}
          height={72}
          className="h-16 w-auto opacity-90"
          priority
        />
        <div className="flex flex-col items-center gap-2">
          <p className="text-5xl font-semibold tracking-tight">404</p>
          <h1 className="text-lg font-medium">This page doesn’t exist</h1>
          <p className="max-w-xs text-sm text-zinc-500 dark:text-zinc-400">
            The page you’re looking for may have moved or never existed.
          </p>
        </div>
        <Link
          href="/"
          className="inline-flex items-center gap-1.5 rounded-lg bg-zinc-900 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-zinc-700 dark:bg-white dark:text-zinc-900 dark:hover:bg-zinc-200"
        >
          <ArrowLeft size={15} />
          Back to home
        </Link>
      </div>
    </main>
  );
}
