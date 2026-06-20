import { NextResponse } from "next/server";
import { start, status } from "../session";
import { ensureNamespace } from "../namespace";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

/** POST /api/run/start { yaml } — render the config and (re)start this user's runner. */
export async function POST(req: Request) {
  const { ns, setCookie } = ensureNamespace(req);
  const withCookie = (res: NextResponse) => {
    if (setCookie) res.headers.set("Set-Cookie", setCookie);
    return res;
  };

  if (!status(ns).available) {
    return withCookie(
      NextResponse.json(
        { error: "Runner not available (OCTO_BIN_PATH unset)." },
        { status: 409 },
      ),
    );
  }
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
    return withCookie(NextResponse.json(await start(ns, yaml)));
  } catch (err) {
    return withCookie(
      NextResponse.json({ error: (err as Error).message }, { status: 500 }),
    );
  }
}
