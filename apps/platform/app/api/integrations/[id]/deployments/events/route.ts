import { stream } from "../../../../orchestrator/client";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Params = { params: Promise<{ id: string }> };

/**
 * GET /api/integrations/:id/deployments/events — proxy the orchestrator's
 * Server-Sent Events stream of the integration's deployment list. The request's
 * AbortSignal is forwarded so closing the browser EventSource closes the upstream
 * connection (and its hub subscription).
 */
export async function GET(req: Request, { params }: Params) {
  const { id } = await params;
  return stream(
    `/integrations/${encodeURIComponent(id)}/deployments/events`,
    req.signal,
  );
}
