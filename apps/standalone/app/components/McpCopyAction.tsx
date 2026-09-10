"use client";

import { useEffect, useState } from "react";
import { CopyMcpUrlButton } from "@octo/editor";
import { desktopBridge } from "../desktop";

/**
 * Where the standalone's MCP endpoint is, for the console header's copy button.
 *
 * The desktop shell is asked first: it owns the port it started the server on, and
 * that is the URL an agent outside the app has to dial. In a browser the endpoint is
 * simply this origin's `/mcp` — the same address the page was loaded from.
 *
 * Resolved in an effect rather than at render, because `window` does not exist on
 * the server and the shell answers asynchronously; until it does, there is no button.
 */
export default function McpCopyAction() {
  const [url, setUrl] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    const bridge = desktopBridge();
    const resolve = async () => {
      const fromShell = bridge ? await bridge.mcpUrl().catch(() => null) : null;
      if (!cancelled) setUrl(fromShell ?? `${window.location.origin}/mcp`);
    };
    void resolve();
    return () => {
      cancelled = true;
    };
  }, []);

  if (!url) return null;
  return <CopyMcpUrlButton url={url} />;
}
