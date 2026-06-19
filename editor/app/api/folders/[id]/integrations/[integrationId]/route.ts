import { proxy } from "../../../../orchestrator/client";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Params = { params: Promise<{ id: string; integrationId: string }> };

/** PUT /api/folders/:id/integrations/:integrationId — add to (single-membership) folder. */
export async function PUT(req: Request, { params }: Params) {
  const { id, integrationId } = await params;
  return proxy(
    req,
    `/folders/${encodeURIComponent(id)}/integrations/${encodeURIComponent(integrationId)}`,
  );
}

/** DELETE /api/folders/:id/integrations/:integrationId — remove from a folder. */
export async function DELETE(req: Request, { params }: Params) {
  const { id, integrationId } = await params;
  return proxy(
    req,
    `/folders/${encodeURIComponent(id)}/integrations/${encodeURIComponent(integrationId)}`,
  );
}
