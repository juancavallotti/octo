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
    let pushed = false;

    // Subscribed as well as read: the download usually finishes long after the page
    // loaded, and the whole point is to notice without a reload.
    const off = bridge.onUpdateStatus((s) => {
      pushed = true;
      setStatus(s);
    });
    bridge
      .updateStatus()
      .then((s) => {
        // A push that already arrived wins. The reply and the push travel separate
        // IPC paths with no ordering between them, and this reply was read before
        // that push was sent — so letting it land would put a stale stage back. It
        // is a one-way loss: "ready" is the last thing the shell ever says, so
        // nothing would come along afterwards to correct it, and the button this
        // whole change exists to show would stay hidden until a reload.
        if (!cancelled && !pushed) setStatus(s);
      })
      .catch(() => {});

    return () => {
      cancelled = true;
      off();
    };
  }, [bridge]);

  if (!bridge || status?.stage !== "ready" || !status.version) return null;

  const label = "Restart to update";

  return (
    <button
      type="button"
      onClick={() => void bridge.installUpdate()}
      aria-label={label}
      title={`Octo ${status.version} is ready — you have ${status.current}`}
      // Save's and Run's shape exactly — it sits beside them and is the same kind
      // of thing: a filled button that acts on the window. Violet rather than their
      // sky and emerald, because it is neither of those actions and the one button
      // here that takes the app away from under you.
      className="inline-flex shrink-0 items-center gap-1.5 rounded-md bg-violet-600 px-3 py-1 text-sm font-medium text-white hover:bg-violet-500"
    >
      <ArrowUpCircle className="h-3.5 w-3.5 shrink-0" />
      {/* Dropped below 1100px, where the bar's centred view tabs reach this far
          across: the window goes down to 900, and the mark is legible on its own
          with the label still in the tooltip and on the button's accessible name.
          Hiding the button itself is not the trade — an update nobody is told
          about is the bug this whole thing fixes. */}
      <span className="hidden min-[1100px]:inline">{label}</span>
    </button>
  );
}
