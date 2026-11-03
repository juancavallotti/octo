import { Check, X } from "lucide-react";
import {
  PLATFORM_CICD,
  PLATFORM_DEVELOPER,
  PLATFORM_MONITOR,
  PLATFORM_OPERATOR,
  ROLE_LABELS,
} from "@/app/auth/roles";

/**
 * What an API key carrying each role may do, for the hover cards on the account
 * page. Worded from the orchestrator's route policy; iam and the orchestrator
 * decide, this only explains.
 */
interface KeyRoleAbility {
  summary: string;
  can: string[];
  cannot: string[];
}

const KEY_ROLE_ABILITIES: Record<string, KeyRoleAbility> = {
  [PLATFORM_MONITOR]: {
    summary: "Monitoring",
    can: ["Read logs, traces and metrics", "Read alerts"],
    cannot: ["Change integrations or deployments"],
  },
  [PLATFORM_DEVELOPER]: {
    summary: "Development",
    can: [
      "Create and change integrations and their resources",
      "Tag versions",
      "Start dev runs and tests",
    ],
    cannot: ["Deploy or operate deployments"],
  },
  [PLATFORM_OPERATOR]: {
    summary: "Operating the platform",
    can: [
      "Upload code and tag versions",
      "Deploy, roll out, scale and remove deployments",
      "Send email",
    ],
    cannot: ["Change secrets, settings or roles"],
  },
  [PLATFORM_CICD]: {
    summary: "Shipping from a pipeline",
    can: [
      "Upload code and tag versions",
      "Run a deployment's tests",
      "Deploy and roll out with the basic identity",
      "Read deployments and their logs",
    ],
    cannot: [
      "Grant a deployment access, which takes a signed-in person",
      "Use the editor, dev runs, secrets or settings",
    ],
  },
};

/** The hover card for a key role, or undefined for a role a key cannot carry. */
export function keyRoleCard(role: string) {
  const ability = KEY_ROLE_ABILITIES[role];
  if (!ability) return undefined;
  return (
    <>
      <span className="flex items-baseline gap-2">
        <span className="text-sm font-semibold text-zinc-900 dark:text-zinc-100">
          {ROLE_LABELS[role] ?? role}
        </span>
        <span className="text-xs text-zinc-500 dark:text-zinc-400">{ability.summary}</span>
      </span>
      <span className="mt-2 flex flex-col gap-1">
        {ability.can.map((line) => (
          <span key={line} className="flex items-start gap-1.5 text-xs text-zinc-700 dark:text-zinc-300">
            <Check size={13} className="mt-px shrink-0 text-emerald-500" />
            {line}
          </span>
        ))}
        {ability.cannot.map((line) => (
          <span key={line} className="flex items-start gap-1.5 text-xs text-zinc-400 dark:text-zinc-500">
            <X size={13} className="mt-px shrink-0" />
            {line}
          </span>
        ))}
      </span>
    </>
  );
}
