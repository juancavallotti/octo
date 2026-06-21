import { proxy } from "../orchestrator/client";
import { withAuth, writeRoles } from "@/app/auth/guard";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

/** GET /api/integrations — list all integrations. */
export function GET(req: Request) {
  return proxy(req, "/integrations");
}

/** POST /api/integrations { name, definition } — create an integration. */
export const POST = withAuth((req: Request) => proxy(req, "/integrations"), {
  roles: writeRoles,
});
