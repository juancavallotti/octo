import { Suspense } from "react";
import AppHeader from "@/app/components/AppHeader";
import ManagementNav from "@/app/components/ManagementNav";
import UserMenu from "@/app/components/UserMenu";
import MetricsTabs from "@/app/components/alerts/MetricsTabs";
import AlertsMonitor from "@/app/components/alerts/AlertsMonitor";

/**
 * The alerts view (`/platform/metrics/alerts`): every watch, and whatever is
 * firing right now.
 *
 * A static segment beside the sibling `[deploymentId]` route, which Next resolves
 * in its favour — and deployment ids are uuids, so the two can never collide in
 * practice either.
 */
export default function AlertsPage() {
  return (
    <div className="flex h-full flex-col">
      <AppHeader userMenu={<UserMenu />}>
        <ManagementNav />
      </AppHeader>
      <MetricsTabs />
      <Suspense fallback={null}>
        <AlertsMonitor />
      </Suspense>
    </div>
  );
}
