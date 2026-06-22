import Image from "next/image";
import Link from "next/link";
import { redirect } from "next/navigation";
import { ArrowRight, BookOpen } from "lucide-react";
import { auth, authEnabled, signIn } from "@/auth";
import CopyCommand from "./CopyCommand";

const DOCS_URL = "https://juancavallotti.github.io/octo/";
const STANDALONE_DOCKER = 'docker run -p 3000:3000 -v "$PWD:/work" juancavallotti/octo';

export const metadata = {
  title: "Octo — visual editor for integrations",
};

/**
 * The public welcome page (`/`). Three states:
 *
 *  - SSO on + signed in  → bounce straight to the dashboard.
 *  - SSO on + signed out → branded landing with a "Sign in with eetr" action that
 *    starts the OIDC flow and returns to the dashboard (or the deep link the
 *    middleware captured as `callbackUrl`).
 *  - SSO off (local dev) → an "Open Octo" link straight into the platform, since
 *    there is no identity provider to sign in against.
 *
 * This page is also configured as Auth.js's `signIn` page, so every "please sign
 * in" path lands here.
 */
export default async function WelcomePage({
  searchParams,
}: {
  searchParams: Promise<{ callbackUrl?: string }>;
}) {
  const { callbackUrl } = await searchParams;
  const target = callbackUrl || "/platform";

  if (authEnabled) {
    const session = await auth();
    if (session?.user) redirect(target);
  }

  return (
    <main className="flex h-full flex-1 items-center justify-center p-6">
      <div className="flex w-full max-w-md flex-col items-center gap-7 rounded-2xl border border-black/10 bg-white/80 p-10 text-center shadow-sm dark:border-white/10 dark:bg-zinc-900/80">
        <Image
          src="/octo-logo.png"
          alt="Octo logo"
          width={128}
          height={128}
          className="h-28 w-auto"
          priority
        />
        <div className="flex flex-col items-center gap-2">
          <h1 className="text-3xl font-semibold tracking-tight">Octo</h1>
          <p className="max-w-xs text-sm text-zinc-500 dark:text-zinc-400">
            Design, deploy, and operate your integrations — all from one visual
            workspace.
          </p>
        </div>

        {authEnabled ? (
          <form
            action={async () => {
              "use server";
              await signIn("eetr", { redirectTo: target });
            }}
            className="w-full"
          >
            <button
              type="submit"
              className="w-full rounded-lg bg-zinc-900 px-4 py-2.5 text-sm font-medium text-white transition-colors hover:bg-zinc-700 dark:bg-white dark:text-zinc-900 dark:hover:bg-zinc-200"
            >
              Sign in with eetr
            </button>
          </form>
        ) : (
          <Link
            href={target}
            className="inline-flex w-full items-center justify-center gap-1.5 rounded-lg bg-zinc-900 px-4 py-2.5 text-sm font-medium text-white transition-colors hover:bg-zinc-700 dark:bg-white dark:text-zinc-900 dark:hover:bg-zinc-200"
          >
            Open Octo
            <ArrowRight size={15} />
          </Link>
        )}

        <a
          href={DOCS_URL}
          target="_blank"
          rel="noreferrer"
          className="inline-flex items-center gap-1.5 text-sm text-sky-600 hover:underline dark:text-sky-400"
        >
          <BookOpen size={15} />
          Read the documentation
        </a>

        <div className="flex w-full flex-col gap-2 border-t border-black/10 pt-6 dark:border-white/10">
          <p className="text-xs font-medium text-zinc-500 dark:text-zinc-400">
            Or try the standalone editor locally — no account needed:
          </p>
          <CopyCommand command={STANDALONE_DOCKER} />
          <p className="text-[11px] text-zinc-400">
            Then open <span className="font-mono">localhost:3000</span> and edit
            flows in the mounted directory.
          </p>
        </div>
      </div>
    </main>
  );
}
