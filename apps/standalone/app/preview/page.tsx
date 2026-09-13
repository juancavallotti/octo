import { notFound } from "next/navigation";
import { probeSchema } from "@octo/run-host";
import type { Capabilities } from "@octo/editor";
import PreviewEditor from "@/app/components/PreviewEditor";

/**
 * Dev-only preview route: `/preview?sample=<name>` renders a repo sample on the editor
 * canvas with neither a filesystem nor a run capability, so nothing can be saved or
 * run. Returns 404 in production builds.
 *
 * The capability schema is probed from the `octo` binary and injected into the client
 * editor, so the palette renders its real blocks; with no runner configured it
 * resolves to null and the editor falls back to an empty bundled palette.
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
