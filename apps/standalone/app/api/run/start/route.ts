import { NextResponse } from "next/server";
import { start, status, ensureNamespace } from "@octo/run-host";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const ENV_NAME = /^[A-Za-z_][A-Za-z0-9_]*$/;

/** Validate the optional `devEnv` map: a plain object of valid env names → string
 * values. Returns the sanitized map, or null if the shape is invalid. */
function parseDevEnv(value: unknown): Record<string, string> | null {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return null;
  }
  const out: Record<string, string> = {};
  for (const [name, val] of Object.entries(value as Record<string, unknown>)) {
    if (!ENV_NAME.test(name) || typeof val !== "string") return null;
    out[name] = val;
  }
  return out;
}

/** POST /api/run/start { yaml, devEnv? } — render the config and (re)start the runner. */
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
  let body: { yaml?: unknown; devEnv?: unknown };
  try {
    body = (await req.json()) ?? {};
  } catch {
    return withCookie(
      NextResponse.json({ error: "invalid JSON body" }, { status: 400 }),
    );
  }
  const yaml = body.yaml;
  if (typeof yaml !== "string" || yaml.trim() === "") {
    return withCookie(
      NextResponse.json({ error: "missing `yaml`" }, { status: 400 }),
    );
  }
  let devEnv: Record<string, string> | undefined;
  if (body.devEnv !== undefined) {
    const parsed = parseDevEnv(body.devEnv);
    if (!parsed) {
      return withCookie(
        NextResponse.json({ error: "invalid `devEnv`" }, { status: 400 }),
      );
    }
    devEnv = parsed;
  }
  try {
    return withCookie(NextResponse.json(await start(ns, yaml, devEnv)));
  } catch (err) {
    return withCookie(
      NextResponse.json({ error: (err as Error).message }, { status: 500 }),
    );
  }
}
