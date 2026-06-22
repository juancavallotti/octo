import EditorShell from "@/app/components/EditorShell";

/**
 * Bookmarkable editor route for a saved integration: `/i/<id>` opens that
 * integration in the editor (loaded client-side by IntegrationLoader). The bare
 * `/` route opens a fresh document.
 */
export default async function IntegrationEditorPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  return <EditorShell integrationId={id} />;
}
