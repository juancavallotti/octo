import { NextResponse } from "next/server";
import { stop, ensureNamespace } from "@octo/run-host";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

/** POST /api/run/stop — stop the runner and clean up its config file. */
export async function POST(req: Request) {
  const { ns, setCookie } = ensureNamespace(req);
  const res = NextResponse.json(await stop(ns));
  if (setCookie) res.headers.set("Set-Cookie", setCookie);
  return res;
}
