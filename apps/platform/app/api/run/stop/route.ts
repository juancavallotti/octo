import { NextResponse } from "next/server";
import { stop } from "../session";
import { ensureNamespace } from "../namespace";
import { withAuth, writeRoles } from "@/app/auth/guard";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

/** POST /api/run/stop — stop this user's runner and clean up its config file. */
export const POST = withAuth(async (req: Request) => {
  const { ns, setCookie } = ensureNamespace(req);
  const res = NextResponse.json(await stop(ns));
  if (setCookie) res.headers.set("Set-Cookie", setCookie);
  return res;
}, { roles: writeRoles });
