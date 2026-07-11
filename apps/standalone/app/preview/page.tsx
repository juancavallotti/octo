import { notFound } from "next/navigation";
import { probeSchema } from "@octo/run-host";
import type { Capabilities } from "@octo/editor";
import PreviewEditor from "@/app/components/PreviewEditor";

/**
 * Dev-only preview route: `/preview?sample=<name>` renders a repo sample on the
 * editor canvas (loaded client-side by PreviewLoader from /api/preview-sample),
 * with neither a filesystem nor a run capability — so it's a pure read-only
 * editor (no Save, no RUN). Used by the Playwright screenshot harness to capture
 * how flows look. Returns 404 in production builds.
 *
 * The capability schema is probed from the bundled `octo` binary (`octo schema`)
 * and injected into the client editor, so the palette renders its real blocks;
 * when no runner is configured this resolves to null and the editor falls back
 * to its bundled JSON (an empty palette). The screenshot harness sets
 * OCTO_BIN_PATH so the palette is populated in the shots.
 */
export default async function PreviewPage({
  searchParams,
}: {
  searchParams: Promise<{ sample?: string }>;
}) {
  if (process.env.NODE_ENV === "production") notFound();
  const { sample } = await searchParams;
  const capabilities = (await probeSchema()) as Capabilities | null;
  return <PreviewEditor sample={sample} capabilities={capabilities} />;
}
