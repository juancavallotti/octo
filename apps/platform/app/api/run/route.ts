import { NextResponse } from "next/server";
import { status } from "./session";
import { probeVersion } from "./version";
import { ensureNamespace } from "./namespace";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

/** GET /api/run — whether RUN is available, whether this user's runner is live, and its version. */
export async function GET(req: Request) {
  await probeVersion(); // warm the version cache so status() can read it
  const { ns, setCookie } = ensureNamespace(req);
  const res = NextResponse.json(status(ns));
  if (setCookie) res.headers.set("Set-Cookie", setCookie);
  return res;
}
