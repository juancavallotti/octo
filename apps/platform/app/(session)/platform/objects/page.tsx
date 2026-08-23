import { Suspense } from "react";
import AppHeader from "@/app/components/AppHeader";
import ManagementNav from "@/app/components/ManagementNav";
import UserMenu from "@/app/components/UserMenu";
import { ConfirmProvider } from "@/app/components/ConfirmDialog";
import ObjectsTabs from "@/app/components/objects/ObjectsTabs";

/**
 * The object store route (`/platform/objects`): browse and manage the user-facing
 * objects each deployment holds, and read how full the two stores holding them are,
 * behind the shared header with the section nav. A server component so it can hand
 * the header the server-rendered account tile; the manager fetches client-side and
 * reads `?deployment`/`?key` from the URL, which needs a Suspense boundary.
 */
export default function ObjectsPage() {
  return (
    <div className="flex h-full flex-col">
      <AppHeader userMenu={<UserMenu />}>
        <ManagementNav />
      </AppHeader>
      <ConfirmProvider>
        <Suspense>
          <ObjectsTabs />
        </Suspense>
      </ConfirmProvider>
    </div>
  );
}
