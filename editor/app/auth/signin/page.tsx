import Image from "next/image";
import { signIn } from "@/auth";

export const metadata = {
  title: "Sign in — Octo",
};

/**
 * Branded sign-in page. The single action starts the eetr OIDC authorization-code
 * flow; on success Auth.js returns to `callbackUrl` (the page the user was trying
 * to reach, set by the middleware).
 */
export default async function SignInPage({
  searchParams,
}: {
  searchParams: Promise<{ callbackUrl?: string }>;
}) {
  const { callbackUrl } = await searchParams;
  return (
    <main className="flex h-full flex-1 items-center justify-center p-6">
      <div className="flex w-full max-w-sm flex-col items-center gap-6 rounded-2xl border border-black/10 bg-white/80 p-8 shadow-sm dark:border-white/10 dark:bg-zinc-900/80">
        <Image
          src="/octo-logo.png"
          alt="Octo logo"
          width={128}
          height={128}
          className="h-32 w-auto"
          priority
        />
        <div className="flex flex-col items-center gap-1">
          <span className="text-2xl font-semibold tracking-tight">Octo</span>
          <p className="text-center text-sm text-zinc-500 dark:text-zinc-400">
            Sign in to access your integrations.
          </p>
        </div>
        <form
          action={async () => {
            "use server";
            await signIn("eetr", { redirectTo: callbackUrl || "/" });
          }}
          className="w-full"
        >
          <button
            type="submit"
            className="w-full rounded-lg bg-zinc-900 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-zinc-700 dark:bg-white dark:text-zinc-900 dark:hover:bg-zinc-200"
          >
            Sign in with eetr
          </button>
        </form>
      </div>
    </main>
  );
}
