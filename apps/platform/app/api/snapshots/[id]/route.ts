import { proxy } from "../../orchestrator/client";
import { withAuth, writeRoles } from "@/app/auth/guard";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Params = { params: Promise<{ id: string }> };

/** DELETE /api/snapshots/:id — delete a version tag. */
export const DELETE = withAuth(async (req: Request, { params }: Params) => {
  const { id } = await params;
  return proxy(req, `/snapshots/${encodeURIComponent(id)}`);
}, { roles: writeRoles });
