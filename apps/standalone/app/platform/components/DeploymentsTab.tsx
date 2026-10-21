"use client";

import { RefreshCw } from "lucide-react";
import type { PlatformState } from "../types";
import { useDeployments } from "../useDeployments";
import { CARD, MUTED, SECONDARY, SECTION_TITLE } from "../ui";
import DeployForm from "./DeployForm";
import DeploymentRow from "./DeploymentRow";
import ErrorLine from "./ErrorLine";

/** Where the open folder's integration runs, and running it somewhere new. */
export default function DeploymentsTab({ state }: { state: PlatformState }) {
  const { link } = state;
  const settled = state.activity?.state === "running" ? undefined : state.activity?.at;
  const { deployments, versions, error, reload } = useDeployments(
    link ? `${link.profile}/${link.integrationId}` : "",
    settled,
  );
  const running = state.activity?.state === "running";

  if (!link) {
    return <p className={MUTED}>Deployments are for a folder that is on a platform. Put this one there from This folder.</p>;
  }

  return (
    <div className="flex flex-col gap-4">
      <section className="flex flex-col gap-2">
        <div className="flex items-center gap-2">
          <h2 className={SECTION_TITLE}>
            {link.integrationName} on {link.profile}
          </h2>
          <span className="flex-1" />
          <button type="button" onClick={reload} className={SECONDARY} title="Reload">
            <RefreshCw size={14} />
          </button>
        </div>
        <ErrorLine error={error} />
        {deployments === null && !error && <p className={MUTED}>Loading…</p>}
        {deployments?.length === 0 && <p className={MUTED}>Not deployed anywhere yet.</p>}
        {deployments && deployments.length > 0 && (
          <ul className={`${CARD} divide-y divide-black/5 dark:divide-white/5`}>
            {deployments.map((d) => (
              <DeploymentRow key={d.id} deployment={d} versions={versions} busy={running} />
            ))}
          </ul>
        )}
      </section>
      <section className="flex flex-col gap-2">
        <h2 className={SECTION_TITLE}>Deploy</h2>
        <DeployForm versions={versions} busy={running} />
      </section>
    </div>
  );
}
