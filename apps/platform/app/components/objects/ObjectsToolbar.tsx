"use client";

import { Database, Lock } from "lucide-react";
import type { DeploymentWithIntegration } from "@/app/model/orchestrator";
import { AppPicker } from "@/app/components/AppPicker";
import { deploymentLabel } from "./format";

/**
 * What is being browsed: a deployment, and one of the namespaces it stores under.
 *
 * The namespace was a second `<select>` that appeared once a deployment had been
 * chosen, which made picking a deployment feel like the first step of a form
 * rather than a choice in its own right. It is an accessory now — beside the
 * deployment, not behind it — and it is *not* folded into the deployment row the
 * way the traces picker folds in a version: a namespace is a place inside one
 * deployment's store, not part of which deployment this is, and there is no
 * (deployment, namespace) pair the store would report as a unit.
 */
export default function ObjectsToolbar({
  deployments,
  deploymentId,
  onSelectDeployment,
  namespaces,
  namespace,
  onSelectNamespace,
  secret,
  onRefresh,
}: {
  deployments: DeploymentWithIntegration[];
  deploymentId: string | null;
  onSelectDeployment: (id: string) => void;
  /** Null until they load; the current one stands in until then. */
  namespaces: string[] | null;
  namespace: string;
  onSelectNamespace: (namespace: string) => void;
  /** Whether the chosen namespace hides its values. */
  secret: boolean;
  onRefresh: () => void;
}) {
  const selected = deployments.find((d) => d.id === deploymentId) ?? null;

  return (
    <AppPicker<DeploymentWithIntegration>
      items={deployments}
      selected={selected}
      onSelect={(d) => onSelectDeployment(d.id)}
      toKey={(d) => d.id}
      toText={(d) => `${deploymentLabel(d)} ${d.id}`}
      renderRow={(d) => <span className="text-sm">{deploymentLabel(d)}</span>}
      renderValue={(d) => deploymentLabel(d)}
      label="Deployment"
      placeholder="Choose a deployment…"
      empty="No deployment is running."
      leading={<Database size={15} className="shrink-0 text-zinc-400" aria-hidden />}
      onRefresh={deploymentId ? onRefresh : undefined}
      accessory={
        deploymentId && (
          <>
            <span className="shrink-0 text-xs font-medium text-zinc-400">namespace</span>
            <select
              value={namespace}
              onChange={(e) => onSelectNamespace(e.target.value)}
              aria-label="Namespace"
              className="shrink-0 rounded-md border border-black/10 bg-transparent px-2 py-1 text-sm dark:border-white/15"
            >
              {(namespaces ?? [namespace]).map((ns) => (
                <option key={ns} value={ns}>
                  {ns}
                </option>
              ))}
            </select>
            {secret && (
              <span
                className="inline-flex shrink-0 items-center gap-1 text-xs font-medium text-amber-600 dark:text-amber-400"
                title="Secret namespace: values are hidden; keys can be cleaned up only"
              >
                <Lock size={12} />
                read-only
              </span>
            )}
          </>
        )
      }
    />
  );
}
