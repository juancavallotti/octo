"use client";

import { useConfirm } from "@/app/components/ConfirmDialog";
import Callout from "@/app/components/ui/Callout";
import type { Environment } from "@/app/model/environments";
import EnvironmentForm from "./EnvironmentForm";
import EnvironmentRow from "./EnvironmentRow";
import { useEnvironments } from "./useEnvironments";

/**
 * The installation's environments: the default one, which is the release
 * namespace, and every one an administrator has created. Creating one
 * provisions its namespace; deleting one removes it, and the orchestrator
 * refuses while anything still runs there.
 */
export default function EnvironmentsManager() {
  const confirm = useConfirm();
  const { environments, busy, error, create, update, remove } = useEnvironments();

  const taken = (name: string) => environments?.some((e) => e.name === name) ?? false;

  const askToDelete = async (environment: Environment) => {
    const ok = await confirm({
      title: `Delete environment "${environment.displayName || environment.name}"?`,
      body: `Its namespace, ${environment.namespace}, is deleted with it. Environments that still run deployments, test runs or dev runs cannot be deleted.`,
      confirmLabel: "Delete",
      danger: true,
    });
    if (ok) await remove(environment.name);
  };

  return (
    <div className="flex h-full flex-col overflow-y-auto px-6 py-5">
      <div className="mx-auto w-full max-w-2xl">
        <h2 className="text-sm font-semibold">Environments</h2>
        <p className="mt-1 text-xs text-zinc-400">
          Each environment runs in a Kubernetes namespace of its own, with its own
          integrations, deployments and secrets. The default environment is the
          namespace this installation runs in.
        </p>

        {error && (
          <div role="alert" className="mt-3">
            <Callout>{error}</Callout>
          </div>
        )}

        <EnvironmentForm busy={busy} taken={taken} onCreate={create} />

        {environments === null ? (
          <p className="px-1 py-6 text-center text-sm text-zinc-400">Loading…</p>
        ) : (
          <ul className="mt-4 flex flex-col gap-1.5">
            {environments.map((e) => (
              <EnvironmentRow
                key={e.name}
                environment={e}
                busy={busy}
                onUpdate={update}
                onDelete={askToDelete}
              />
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
