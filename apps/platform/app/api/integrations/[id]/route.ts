import { proxy } from "../../orchestrator/client";
import { withAuth, writeRoles } from "@/app/auth/guard";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Params = { params: Promise<{ id: string }> };

/** GET /api/integrations/:id — fetch one integration. */
export async function GET(req: Request, { params }: Params) {
  const { id } = await params;
  return proxy(req, `/integrations/${encodeURIComponent(id)}`);
}

/** PUT /api/integrations/:id { name, definition } — update an integration. */
export const PUT = withAuth(async (req: Request, { params }: Params) => {
  const { id } = await params;
  return proxy(req, `/integrations/${encodeURIComponent(id)}`);
}, { roles: writeRoles });

/** DELETE /api/integrations/:id — delete an integration. */
export const DELETE = withAuth(async (req: Request, { params }: Params) => {
  const { id } = await params;
  return proxy(req, `/integrations/${encodeURIComponent(id)}`);
}, { roles: writeRoles });
