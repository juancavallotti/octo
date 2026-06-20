import { proxy } from "../../orchestrator/client";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Params = { params: Promise<{ id: string }> };

/** GET /api/deployments/:id — fetch one deployment (status refreshed on read). */
export async function GET(req: Request, { params }: Params) {
  const { id } = await params;
  return proxy(req, `/deployments/${encodeURIComponent(id)}`);
}

/** PATCH /api/deployments/:id — scale (change the desired replica count). */
export async function PATCH(req: Request, { params }: Params) {
  const { id } = await params;
  return proxy(req, `/deployments/${encodeURIComponent(id)}`);
}

/** DELETE /api/deployments/:id — undeploy. */
export async function DELETE(req: Request, { params }: Params) {
  const { id } = await params;
  return proxy(req, `/deployments/${encodeURIComponent(id)}`);
}
