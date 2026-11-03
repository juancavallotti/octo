import AppHeader from "@/app/components/AppHeader";
import UserMenu from "@/app/components/UserMenu";
import UpgradesView from "@/app/components/upgrades/UpgradesView";

/**
 * The upgrades route (`/platform/upgrades`): every deployment running an older
 * runtime than this install deploys, with its test suites run against the new
 * one, and every deployment that has no suites to run. Reached from the user
 * menu, so it sits behind the header without the section nav.
 */
export default function UpgradesPage() {
  return (
    <div className="flex h-full flex-col">
      <AppHeader userMenu={<UserMenu />} />
      <div className="min-h-0 flex-1 overflow-y-auto">
        <UpgradesView />
      </div>
    </div>
  );
}
