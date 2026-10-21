import type { Metadata } from "next";
import PlatformApp from "./components/PlatformApp";

export const metadata: Metadata = { title: "Platform — Octo" };

/**
 * `/platform` is the desktop shell's Platform window: the installations, their
 * integrations, and the open folder's versions, deployments and logs. Only the shell
 * can drive the platform — it runs the octopaas CLI — so in a browser the page says
 * where to find it.
 */
export default async function PlatformPage({
  searchParams,
}: {
  searchParams: Promise<{ focus?: string }>;
}) {
  const { focus } = await searchParams;
  return <PlatformApp focus={focus} />;
}
