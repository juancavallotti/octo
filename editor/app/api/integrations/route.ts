import { proxy } from "../orchestrator/client";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

/** GET /api/integrations — list all integrations. */
export function GET(req: Request) {
  return proxy(req, "/integrations");
}

/** POST /api/integrations { name, definition } — create an integration. */
export function POST(req: Request) {
  return proxy(req, "/integrations");
}
