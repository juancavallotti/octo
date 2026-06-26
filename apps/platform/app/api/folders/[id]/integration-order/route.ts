import { proxy } from "../../../orchestrator/client";
import { withAuth, writeRoles } from "@/app/auth/guard";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Params = { params: Promise<{ id: string }> };

/** PUT /api/folders/:id/integration-order — persist the order of a folder's integrations. */
export const PUT = withAuth(async (req: Request, { params }: Params) => {
  const { id } = await params;
  return proxy(req, `/folders/${encodeURIComponent(id)}/integration-order`);
}, { roles: writeRoles });
