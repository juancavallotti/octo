import PlatformServices from "@/app/components/admin/PlatformServices";

/**
 * The platform-services report (`/platform/admin/health`). No ConfirmProvider:
 * this page has no action that changes anything.
 */
export default function AdminHealthPage() {
  return <PlatformServices />;
}
