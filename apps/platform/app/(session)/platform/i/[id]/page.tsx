import PlatformEditor from "@/app/components/PlatformEditor";
import UserMenu from "@/app/components/UserMenu";

/**
 * Bookmarkable editor route for a saved integration: `/platform/i/<id>` opens that
 * integration in the editor (loaded client-side by IntegrationLoader).
 * `/platform/new` opens a fresh document.
 */
export default async function IntegrationEditorPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  return <PlatformEditor integrationId={id} userMenu={<UserMenu />} />;
}
