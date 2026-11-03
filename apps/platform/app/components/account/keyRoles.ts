import { PLATFORM_DEVELOPER, PLATFORM_MONITOR, PLATFORM_OPERATOR } from "@/app/auth/roles";

/**
 * What an API key carrying each role may do, for the tooltips on the account
 * page. Worded from the orchestrator's route policy; iam and the orchestrator
 * decide, this only explains.
 */
export const KEY_ROLE_ABILITIES: Record<string, string> = {
  [PLATFORM_MONITOR]:
    "Read only: integrations, versions, deployments, logs, traces and metrics. Cannot change anything.",
  [PLATFORM_DEVELOPER]:
    "Everything Monitor can, plus create and change integrations, their resources and versions, and start dev runs. Cannot deploy.",
  [PLATFORM_OPERATOR]:
    "Everything Developer can, plus deploy, roll out, scale and remove deployments, and send email.",
};
