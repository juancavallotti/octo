import { proxy } from "../../../../orchestrator/client";
import { withAuth, writeRoles } from "@/app/auth/guard";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Params = { params: Promise<{ id: string; integrationId: string }> };

/** PUT /api/folders/:id/integrations/:integrationId — add to (single-membership) folder. */
export const PUT = withAuth(async (req: Request, { params }: Params) => {
  const { id, integrationId } = await params;
  return proxy(
    req,
    `/folders/${encodeURIComponent(id)}/integrations/${encodeURIComponent(integrationId)}`,
  );
}, { roles: writeRoles });

/** DELETE /api/folders/:id/integrations/:integrationId — remove from a folder. */
export const DELETE = withAuth(async (req: Request, { params }: Params) => {
  const { id, integrationId } = await params;
  return proxy(
    req,
    `/folders/${encodeURIComponent(id)}/integrations/${encodeURIComponent(integrationId)}`,
  );
}, { roles: writeRoles });
