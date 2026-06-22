import { proxy } from "../../orchestrator/client";
import { withAuth, writeRoles } from "@/app/auth/guard";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Params = { params: Promise<{ name: string }> };

/** PUT /api/secrets/:name — create or overwrite a secret's value (write-only). */
export const PUT = withAuth(async (req: Request, { params }: Params) => {
  const { name } = await params;
  return proxy(req, `/secrets/${encodeURIComponent(name)}`);
}, { roles: writeRoles });

/** DELETE /api/secrets/:name — delete a secret; ?force=true overrides the in-use guard. */
export const DELETE = withAuth(async (req: Request, { params }: Params) => {
  const { name } = await params;
  const force = new URL(req.url).searchParams.get("force") === "true";
  return proxy(
    req,
    `/secrets/${encodeURIComponent(name)}${force ? "?force=true" : ""}`,
  );
}, { roles: writeRoles });
