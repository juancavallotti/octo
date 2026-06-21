import { proxy } from "../../orchestrator/client";
import { withAuth, writeRoles } from "@/app/auth/guard";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Params = { params: Promise<{ id: string }> };

/** PUT /api/folders/:id { name, parentId } — rename / re-parent a folder. */
export const PUT = withAuth(async (req: Request, { params }: Params) => {
  const { id } = await params;
  return proxy(req, `/folders/${encodeURIComponent(id)}`);
}, { roles: writeRoles });

/** DELETE /api/folders/:id — delete a folder. */
export const DELETE = withAuth(async (req: Request, { params }: Params) => {
  const { id } = await params;
  return proxy(req, `/folders/${encodeURIComponent(id)}`);
}, { roles: writeRoles });
