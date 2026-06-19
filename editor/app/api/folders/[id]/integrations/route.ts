import { proxy } from "../../../orchestrator/client";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Params = { params: Promise<{ id: string }> };

/** GET /api/folders/:id/integrations — integrations in a folder. */
export async function GET(req: Request, { params }: Params) {
  const { id } = await params;
  return proxy(req, `/folders/${encodeURIComponent(id)}/integrations`);
}
