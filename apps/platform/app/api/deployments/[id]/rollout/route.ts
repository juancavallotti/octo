import { proxy } from "../../../orchestrator/client";
import { withAuth, writeRoles } from "@/app/auth/guard";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Params = { params: Promise<{ id: string }> };

/** POST /api/deployments/:id/rollout — roll the deployment over to another version tag. */
export const POST = withAuth(async (req: Request, { params }: Params) => {
  const { id } = await params;
  return proxy(req, `/deployments/${encodeURIComponent(id)}/rollout`);
}, { roles: writeRoles });
