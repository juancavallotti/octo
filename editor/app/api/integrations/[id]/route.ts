import { proxy } from "../../orchestrator/client";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Params = { params: Promise<{ id: string }> };

/** GET /api/integrations/:id — fetch one integration. */
export async function GET(req: Request, { params }: Params) {
  const { id } = await params;
  return proxy(req, `/integrations/${encodeURIComponent(id)}`);
}

/** PUT /api/integrations/:id { name, definition } — update an integration. */
export async function PUT(req: Request, { params }: Params) {
  const { id } = await params;
  return proxy(req, `/integrations/${encodeURIComponent(id)}`);
}

/** DELETE /api/integrations/:id — delete an integration. */
export async function DELETE(req: Request, { params }: Params) {
  const { id } = await params;
  return proxy(req, `/integrations/${encodeURIComponent(id)}`);
}
