"use client";

import { useEffect, useState } from "react";
import { ArrowUpCircle } from "lucide-react";
import { desktopBridge, type UpdateStatusView } from "../desktop";

/**
 * The new version waiting to be installed, and the click that installs it.
 *
 * An update only lands on a restart, and nothing else in the app ever asks to be
 * restarted — so without a standing invitation to do it, the download just sits there.
 * This is that invitation, in the corner VS Code puts it in. It appears only once the
 * shell says the update is downloaded: an offer to restart into something still
 * arriving would restart into nothing.
 */
export default function UpdateChip() {
  const bridge = desktopBridge();
  const [status, setStatus] = useState<UpdateStatusView | null>(null);

  useEffect(() => {
    // Absent on a shell older than this page — the two are declared against each
    // other, not compiled together.
    if (!bridge || typeof bridge.updateStatus !== "function") return;
    let cancelled = false;
    bridge
      .updateStatus()
      .then((s) => {
        if (!cancelled) setStatus(s);
      })
      .catch(() => {});
    // Subscribed as well as read: the download usually finishes long after the page
    // loaded, and the whole point is to notice without a reload.
    const off = bridge.onUpdateStatus((s) => setStatus(s));
    return () => {
      cancelled = true;
      off();
    };
  }, [bridge]);

  if (!bridge || status?.stage !== "ready" || !status.version) return null;

  return (
    <button
      type="button"
      onClick={() => void bridge.installUpdate()}
      title={`Octo ${status.version} is ready — you have ${status.current}`}
      className="flex items-center gap-1.5 rounded-md bg-emerald-500/10 px-2 py-1 text-[13px] font-medium text-emerald-700 transition-colors hover:bg-emerald-500/20 dark:text-emerald-300"
    >
      <ArrowUpCircle size={14} className="shrink-0" />
      Restart to update
    </button>
  );
}
