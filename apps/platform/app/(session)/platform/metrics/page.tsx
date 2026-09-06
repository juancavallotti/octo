import AppHeader from "@/app/components/AppHeader";
import ManagementNav from "@/app/components/ManagementNav";
import UserMenu from "@/app/components/UserMenu";
import DeploymentsMonitor from "@/app/components/deployments/DeploymentsMonitor";
import {
  CURRENT_RUNTIME_IMAGE,
  CURRENT_RUNTIME_VERSION,
  currentRuntime,
} from "@/app/lib/runtimeRelease";

/**
 * The metrics route (`/platform/metrics`): every active deployment across
 * all integrations with live status, behind the shared header and section nav. A
 * server component so it can hand the header the server-rendered account tile and
 * the runtime this install deploys (read from server-only env), matching the
 * sibling routes; the monitor fetches the deployments client-side.
 */
export default function DeploymentsPage() {
  return (
    <div className="flex h-full flex-col">
      <AppHeader userMenu={<UserMenu />}>
        <ManagementNav />
      </AppHeader>
      <DeploymentsMonitor
        currentRuntime={currentRuntime(CURRENT_RUNTIME_VERSION, CURRENT_RUNTIME_IMAGE)}
      />
    </div>
  );
}
