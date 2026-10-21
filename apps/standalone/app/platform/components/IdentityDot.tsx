import type { Identity } from "../types";

/** Whether an installation knows who the user is: the one-glance answer, with words in its title. */
export default function IdentityDot({ identity }: { identity: Identity }) {
  const [color, title] =
    identity.state === "signed-in"
      ? ["bg-emerald-500", `Signed in as ${identity.email || identity.name}`]
      : identity.state === "checking"
        ? ["bg-zinc-300 animate-pulse dark:bg-zinc-600", "Checking…"]
        : identity.state === "error"
          ? ["bg-amber-500", identity.message]
          : ["bg-zinc-300 dark:bg-zinc-600", "Not signed in"];
  return <span title={title} aria-label={title} className={`inline-block h-2 w-2 shrink-0 rounded-full ${color}`} />;
}
