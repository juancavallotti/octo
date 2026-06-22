import UserMenu from "@/app/components/UserMenu";
import Dashboard from "./Dashboard";

/**
 * The platform landing page (`/platform`): the dashboard. A server component so it
 * can hand the client dashboard the server-rendered account tile.
 */
export default function DashboardPage() {
  return <Dashboard userMenu={<UserMenu />} />;
}
