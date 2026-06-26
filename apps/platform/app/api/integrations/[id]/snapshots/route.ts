import { proxy } from "../../../orchestrator/client";
import { withAuth, writeRoles } from "@/app/auth/guard";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Params = { params: Promise<{ id: string }> };

/** GET /api/integrations/:id/snapshots — list an integration's version tags. */
export async function GET(req: Request, { params }: Params) {
  const { id } = await params;
  return proxy(req, `/integrations/${encodeURIComponent(id)}/snapshots`);
}

/** POST /api/integrations/:id/snapshots — tag the integration's current definition. */
export const POST = withAuth(async (req: Request, { params }: Params) => {
  const { id } = await params;
  return proxy(req, `/integrations/${encodeURIComponent(id)}/snapshots`);
}, { roles: writeRoles });
