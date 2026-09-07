import { Suspense } from "react";
import AppHeader from "@/app/components/AppHeader";
import ManagementNav from "@/app/components/ManagementNav";
import UserMenu from "@/app/components/UserMenu";
import { ConfirmProvider } from "@/app/components/ConfirmDialog";
import MetricsTabs from "@/app/components/alerts/MetricsTabs";
import WatchPage from "@/app/components/alerts/WatchPage";

/**
 * One watch (`/platform/metrics/alerts/{id}`): its definition, and everything it
 * has done.
 *
 * The literal id `new` opens an unsaved watch. A path segment rather than a
 * query parameter because it is a different page rather than a mode of this one,
 * and watch ids are uuids so the two cannot collide.
 *
 * ConfirmProvider is here because deleting a watch takes its evaluation history
 * and every episode it recorded with it, and nothing here can be undone.
 */
export default async function WatchRoute({
  params,
}: {
  params: Promise<{ watchId: string }>;
}) {
  const { watchId } = await params;
  return (
    <div className="flex h-full flex-col">
      <AppHeader userMenu={<UserMenu />}>
        <ManagementNav />
      </AppHeader>
      <MetricsTabs />
      <ConfirmProvider>
        <Suspense fallback={null}>
          <WatchPage watchId={watchId} />
        </Suspense>
      </ConfirmProvider>
    </div>
  );
}
