"use client";

import { ShieldCheck, Terminal } from "lucide-react";
import {
  DEPLOYMENT_ACCESS,
  type DeploymentAccess,
} from "@/app/model/orchestratorTypes";

/**
 * The two pills that say a deployment is more than an ordinary one: what its own
 * token opens on the platform, and whether its pods carry a shell.
 *
 * They share a tint, and it is the same amber the deploy dialog warns in, because
 * they are the same facts read back. Almost no deployment wears either, which is
 * exactly what makes one worth noticing in a list.
 */

const PRIVILEGED =
  "inline-flex items-center gap-1 rounded-full bg-amber-500/15 px-2 py-0.5 " +
  "text-xs font-medium text-amber-600 dark:text-amber-400";

/**
 * A grant this deployment's own token carries on the platform it runs on.
 *
 * A pod that may rewrite anyone's integration, or tear down anyone's deployment,
 * is a fact about the thing in front of you — and until this pill existed it
 * lived only in the dialog that asked for it. An unrecognized grant still renders,
 * under its own name: a platform that learns a new one must not go quiet about it
 * in a UI that has not caught up.
 */
export function AccessPill({ grant }: { grant: DeploymentAccess }) {
  const known = DEPLOYMENT_ACCESS.find((a) => a.value === grant);
  return (
    <span
      className={PRIVILEGED}
      title={known?.detail ?? `This deployment's token holds ${grant}.`}
    >
      <ShieldCheck size={10} />
      {known?.label ?? grant}
    </span>
  );
}

/**
 * Shown only for the agentic runner — a deployment wearing no runner pill is on
 * the default distroless image, which is what almost all of them are.
 */
export function RunnerPill() {
  return (
    <span
      className={PRIVILEGED}
      title="Agentic runner: a pod carrying a shell, curl, jq, the octo CLI and a writable workspace. Privileged rather than just bigger — it can run anything the pod can reach."
    >
      <Terminal size={10} />
      Agentic
    </span>
  );
}
