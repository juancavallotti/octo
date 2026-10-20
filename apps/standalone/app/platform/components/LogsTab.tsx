"use client";

import { useEffect, useRef, useState } from "react";
import { Play, Square } from "lucide-react";
import { TerminalText } from "@octo/editor";
import { paasBridge } from "../../desktop";
import type { PlatformState } from "../types";
import { useAction } from "../useAction";
import { useDeployments } from "../useDeployments";
import { CARD, INPUT, MUTED, PRIMARY, SECONDARY } from "../ui";
import ErrorLine from "./ErrorLine";

const KEEP = 2000;

/** A deployment's log: what it has stored, then every line its pods print while followed. */
export default function LogsTab({ state }: { state: PlatformState }) {
  const paas = paasBridge();
  const { link } = state;
  const { deployments } = useDeployments(link ? `${link.profile}/${link.integrationId}` : "", undefined);
  const [chosen, setChosen] = useState("");
  const [lines, setLines] = useState<string[]>([]);
  const [following, setFollowing] = useState(false);
  const { busy, error, run } = useAction();
  const end = useRef<HTMLDivElement>(null);
  const id = chosen || deployments?.[0]?.id || "";

  useEffect(() => {
    if (!paas) return;
    return paas.onLog((l) => {
      if (l.deploymentId === id) setLines((all) => [...all, l.line].slice(-KEEP));
    });
  }, [paas, id]);

  useEffect(() => () => void paasBridge()?.unfollow(), []);
  useEffect(() => end.current?.scrollIntoView({ block: "end" }), [lines]);

  if (!link) return <p className={MUTED}>Logs are for a folder that is on a platform.</p>;

  const load = () =>
    paas &&
    void run(async () => {
      const entries = await paas.logs(id);
      setLines(entries.map((e) => [e.ts, e.level?.toUpperCase(), e.message].filter(Boolean).join("  ")));
    });
  const toggle = () =>
    paas &&
    void run(async () => {
      if (following) await paas.unfollow();
      else await paas.follow(id);
      setFollowing(!following);
    });

  return (
    <div className="flex min-h-0 flex-col gap-2">
      <div className="flex items-center gap-2">
        <select value={id} onChange={(e) => { setChosen(e.target.value); setLines([]); setFollowing(false); void paas?.unfollow(); }} aria-label="Deployment" className={INPUT}>
          {(deployments ?? []).map((d) => (
            <option key={d.id} value={d.id}>
              {d.name} ({d.tag})
            </option>
          ))}
        </select>
        <button type="button" disabled={!id || busy} onClick={load} className={SECONDARY}>
          Show stored
        </button>
        <button type="button" disabled={!id || busy} onClick={toggle} className={following ? SECONDARY : PRIMARY}>
          {following ? <Square size={12} /> : <Play size={12} />} {following ? "Stop following" : "Follow"}
        </button>
      </div>
      <ErrorLine error={error} />
      {deployments?.length === 0 && <p className={MUTED}>Not deployed anywhere yet.</p>}
      <div className={`${CARD} h-[28rem] overflow-y-auto bg-zinc-50 p-3 font-mono text-xs leading-5 dark:bg-zinc-950`}>
        {lines.map((l, i) => (
          <div key={i} className="whitespace-pre-wrap break-all">
            <TerminalText text={l} />
          </div>
        ))}
        <div ref={end} />
      </div>
    </div>
  );
}
