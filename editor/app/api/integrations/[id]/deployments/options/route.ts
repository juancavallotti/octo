import { proxy } from "../../../../orchestrator/client";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Params = { params: Promise<{ id: string }> };

/**
 * GET /api/integrations/:id/deployments/options — deploy choices for the modal:
 * whether the integration is networked plus a suggested free slug, or (with
 * ?slug=&expose=) live validation of a candidate slug. The query string is
 * forwarded to the orchestrator.
 */
export async function GET(req: Request, { params }: Params) {
  const { id } = await params;
  const qs = new URL(req.url).search;
  return proxy(req, `/integrations/${encodeURIComponent(id)}/deployments/options${qs}`);
}
