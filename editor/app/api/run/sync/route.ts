import { NextResponse } from "next/server";
import { sync } from "../session";
import { ensureNamespace } from "../namespace";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

/** POST /api/run/sync { yaml } — rewrite this user's watched config so the runner reloads. */
export async function POST(req: Request) {
  const { ns, setCookie } = ensureNamespace(req);
  const withCookie = (res: NextResponse) => {
    if (setCookie) res.headers.set("Set-Cookie", setCookie);
    return res;
  };

  let yaml: unknown;
  try {
    yaml = (await req.json())?.yaml;
  } catch {
    return withCookie(NextResponse.json({ error: "invalid JSON body" }, { status: 400 }));
  }
  if (typeof yaml !== "string" || yaml.trim() === "") {
    return withCookie(NextResponse.json({ error: "missing `yaml`" }, { status: 400 }));
  }
  try {
    return withCookie(NextResponse.json(await sync(ns, yaml)));
  } catch (err) {
    return withCookie(
      NextResponse.json({ error: (err as Error).message }, { status: 500 }),
    );
  }
}
