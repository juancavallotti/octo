import AppHeader from "@/app/components/AppHeader";
import ManagementNav from "@/app/components/ManagementNav";
import UserMenu from "@/app/components/UserMenu";
import MetricsTabs from "@/app/components/alerts/MetricsTabs";
import DeploymentsMonitor from "@/app/components/deployments/DeploymentsMonitor";

/**
 * The metrics route (`/platform/metrics`): every active deployment across
 * all integrations with live status, behind the shared header and section nav —
 * and, since alerting landed, a tab strip, because a watch is a standing question
 * about exactly what this page already shows. A
 * server component so it can hand the header the server-rendered account tile,
 * matching the sibling routes; the monitor fetches the deployments client-side.
 */
export default function DeploymentsPage() {
  return (
    <div className="flex h-full flex-col">
      <AppHeader userMenu={<UserMenu />}>
        <ManagementNav />
      </AppHeader>
      <MetricsTabs />
      <DeploymentsMonitor />
    </div>
  );
}
