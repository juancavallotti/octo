import { Suspense } from "react";
import AppHeader from "@/app/components/AppHeader";
import ManagementNav from "@/app/components/ManagementNav";
import UserMenu from "@/app/components/UserMenu";
import MetricsMonitor from "@/app/components/metrics/MetricsMonitor";

/**
 * One deployment's metrics (`/platform/metrics/{deploymentId}`): the CPU and
 * memory its pods have been recording, over anything from the last five minutes
 * to the last week.
 *
 * Reached from a deployment rather than from the management nav, because the view
 * has no meaning without one — the stats are stored deployment-first, and a nav
 * tab would land somewhere with nothing selected. A server component so the header
 * gets the server-rendered account tile, matching the sibling management routes.
 */
export default async function DeploymentMetricsPage({
  params,
}: {
  params: Promise<{ deploymentId: string }>;
}) {
  const { deploymentId } = await params;

  return (
    <div className="flex h-full flex-col">
      <AppHeader userMenu={<UserMenu />}>
        <ManagementNav />
      </AppHeader>
      {/* MetricsMonitor reads the range from the URL via useSearchParams, which
          needs a Suspense boundary. */}
      <Suspense>
        <MetricsMonitor deploymentId={decodeURIComponent(deploymentId)} />
      </Suspense>
    </div>
  );
}
