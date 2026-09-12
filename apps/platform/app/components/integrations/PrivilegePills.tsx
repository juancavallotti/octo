"use client";

import { Boxes, Rocket, ShieldCheck, Terminal } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import {
  DEPLOYMENT_ACCESS,
  type DeploymentAccess,
} from "@/app/model/orchestratorTypes";

/**
 * The pills that say a deployment is more than an ordinary one: what its own
 * token opens on the platform, and whether its pods carry a shell.
 *
 * All three stay in the warm register the deploy dialog warns in, because they
 * are the same facts read back and none of them should look benign. They differ
 * within it so a row carrying two can be told apart at a glance — and each
 * carries its own icon as well as its own tint, because a reader who cannot
 * separate amber from rose should not be reading a label that says the opposite
 * of what they think.
 *
 * Almost no deployment wears any of these, which is exactly what makes one worth
 * noticing in a list.
 */

const PILL =
  "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium";

/**
 * What each grant looks like. Keyed by the grant, so the two never collide, and
 * exported so the dialog that hands a grant out draws it the same way the pill
 * that reports it does.
 */
export const GRANTS: Record<DeploymentAccess, { tint: string; Icon: LucideIcon }> = {
  // Rewrites integrations across the installation — the wide one, not the
  // destructive one.
  developer: {
    tint: "bg-orange-500/15 text-orange-600 dark:text-orange-400",
    Icon: Boxes,
  },
  // Tears deployments down, anyone's included. The nearest thing here to a red.
  operator: {
    tint: "bg-rose-500/15 text-rose-600 dark:text-rose-400",
    Icon: Rocket,
  },
};

/**
 * A grant this deployment's own token carries on the platform it runs on.
 *
 * A pod that may rewrite anyone's integration, or tear down anyone's deployment,
 * is a fact about the thing in front of you — and until this pill existed it
 * lived only in the dialog that asked for it. An unrecognized grant still
 * renders, under its own name and in the plain privileged tint: a platform that
 * learns a new grant must not go quiet about it in a UI that has not caught up.
 */
export function AccessPill({ grant }: { grant: DeploymentAccess }) {
  const known = DEPLOYMENT_ACCESS.find((a) => a.value === grant);
  const style = GRANTS[grant];
  const Icon = style?.Icon ?? ShieldCheck;
  return (
    <span
      className={`${PILL} ${style?.tint ?? "bg-amber-500/15 text-amber-600 dark:text-amber-400"}`}
      title={known?.detail ?? `This deployment's token holds ${grant}.`}
    >
      <Icon size={10} />
      {known?.label ?? grant}
    </span>
  );
}

/**
 * Shown only for the agentic runner — a deployment wearing no runner pill is on
 * the default distroless image, which is what almost all of them are.
 *
 * Amber, and apart from the grants on purpose: this one is about what the pod
 * IS rather than what its token opens, and the two are independent.
 */
export function RunnerPill() {
  return (
    <span
      className={`${PILL} bg-amber-500/15 text-amber-600 dark:text-amber-400`}
      title="Agentic runner: a pod carrying a shell, curl, jq, the octo CLI and a writable workspace. Privileged rather than just bigger — it can run anything the pod can reach."
    >
      <Terminal size={10} />
      Agentic
    </span>
  );
}
