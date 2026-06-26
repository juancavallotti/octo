import { proxy } from "../../orchestrator/client";
import { withAuth, writeRoles } from "@/app/auth/guard";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

/** PUT /api/folders/reorder — persist the order of one parent's folders. */
export const PUT = withAuth(async (req: Request) => {
  return proxy(req, "/folders/reorder");
}, { roles: writeRoles });
