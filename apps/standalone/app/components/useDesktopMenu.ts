"use client";

import { useEffect } from "react";
import { useNewItem } from "@octo/editor";
import { desktopBridge } from "@/app/desktop";

/**
 * Carry out the hosting shell's File → New on the editor: the shell names what to
 * start, and the editor makes it. Nothing happens in a plain browser, which has no
 * menu to listen to.
 */
export function useDesktopMenu(): void {
  const newItem = useNewItem();

  useEffect(() => {
    const bridge = desktopBridge();
    // Checked at runtime: an older shell reaches here typed as if it had this method.
    if (typeof bridge?.onNewItem !== "function") return;
    return bridge.onNewItem(newItem);
  }, [newItem]);
}
