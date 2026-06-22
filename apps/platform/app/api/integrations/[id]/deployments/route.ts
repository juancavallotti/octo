import { proxy } from "../../../orchestrator/client";
import { withAuth, writeRoles } from "@/app/auth/guard";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Params = { params: Promise<{ id: string }> };

/** GET /api/integrations/:id/deployments — list an integration's deployments. */
export async function GET(req: Request, { params }: Params) {
  const { id } = await params;
  return proxy(req, `/integrations/${encodeURIComponent(id)}/deployments`);
}

/** POST /api/integrations/:id/deployments — deploy the integration. */
export const POST = withAuth(async (req: Request, { params }: Params) => {
  const { id } = await params;
  return proxy(req, `/integrations/${encodeURIComponent(id)}/deployments`);
}, { roles: writeRoles });
