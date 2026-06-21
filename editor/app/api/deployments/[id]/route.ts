import { proxy } from "../../orchestrator/client";
import { withAuth, writeRoles } from "@/app/auth/guard";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Params = { params: Promise<{ id: string }> };

/** GET /api/deployments/:id — fetch one deployment (status refreshed on read). */
export async function GET(req: Request, { params }: Params) {
  const { id } = await params;
  return proxy(req, `/deployments/${encodeURIComponent(id)}`);
}

/** PATCH /api/deployments/:id — scale (change the desired replica count). */
export const PATCH = withAuth(async (req: Request, { params }: Params) => {
  const { id } = await params;
  return proxy(req, `/deployments/${encodeURIComponent(id)}`);
}, { roles: writeRoles });

/** DELETE /api/deployments/:id — undeploy. */
export const DELETE = withAuth(async (req: Request, { params }: Params) => {
  const { id } = await params;
  return proxy(req, `/deployments/${encodeURIComponent(id)}`);
}, { roles: writeRoles });
