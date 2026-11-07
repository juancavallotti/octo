"use client";

import { useEffect, useRef, useState } from "react";
import { ArrowUpRight, Layers } from "lucide-react";
import { useEnvironment } from "@/app/auth/EnvironmentContext";
import { listEnvironments, selectEnvironment } from "@/app/model/environments";
import { promoteSnapshot, type Promotion, type Snapshot } from "@/app/model/orchestrator";
import { reloadAt } from "@/app/components/environmentSwitch";
import Callout from "@/app/components/ui/Callout";
import { promoteTargets, type PromoteTarget } from "./promoteTargets";

/**
 * Promote the version in view into another environment. A working copy is not a
 * version, so with it in view the button is disabled and says to pick one. The
 * menu offers the environments the person builds in; once a promotion lands it
 * says where, with a way to go and look.
 *
 * Absent entirely where there is nowhere to promote to, so an installation with
 * one environment shows nothing new.
 */
export default function PromoteButton({ snapshot, busy }: { snapshot: Snapshot | null; busy: boolean }) {
  const context = useEnvironment();
  const [targets, setTargets] = useState<PromoteTarget[] | null>(null);
  const [open, setOpen] = useState(false);
  const [working, setWorking] = useState(false);
  const [done, setDone] = useState<{ promotion: Promotion; label: string } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!context) return;
    let live = true;
    listEnvironments().then(
      (all) => live && setTargets(promoteTargets(all, context.environment, context.roles, context.envRoles)),
      () => live && setTargets([]),
    );
    return () => {
      live = false;
    };
  }, [context]);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && setOpen(false);
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [open]);

  if (!targets || targets.length === 0) return null;

  const promote = async (target: PromoteTarget) => {
    if (!snapshot) return;
    setWorking(true);
    setError(null);
    try {
      setDone({ promotion: await promoteSnapshot(snapshot.id, target.name), label: target.label });
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setWorking(false);
    }
  };

  // A full load rather than a client-side push, as the top-bar switcher does:
  // every list on the page was read in the environment being left, and only a
  // fresh load is sure to drop all of them.
  const openThere = async (p: Promotion) => {
    setError(null);
    try {
      await selectEnvironment(p.environment);
    } catch (e) {
      setError((e as Error).message);
      return;
    }
    reloadAt(`/platform/integrations/i/${encodeURIComponent(p.integrationId)}`);
  };

  return (
    <div ref={ref} className="relative shrink-0">
      <button
        type="button"
        onClick={() => {
          setDone(null);
          setError(null);
          setOpen((v) => !v);
        }}
        disabled={busy || working || !snapshot}
        title={snapshot ? `Promote ${snapshot.tag} to another environment` : "Pick a version to promote"}
        className="inline-flex items-center gap-1.5 rounded-md border border-black/10 px-3 py-1 text-sm font-medium text-zinc-700 transition-colors hover:bg-black/[0.04] disabled:opacity-50 dark:border-white/15 dark:text-zinc-200 dark:hover:bg-white/[0.06]"
      >
        <ArrowUpRight size={14} />
        Promote
      </button>
      {open && snapshot && (
        <div
          role="menu"
          aria-label={`Promote ${snapshot.tag}`}
          className="absolute right-0 z-50 mt-1 w-72 overflow-hidden rounded-lg border border-black/10 bg-white shadow-lg dark:border-white/10 dark:bg-zinc-900"
        >
          <p className="border-b border-black/5 px-3 py-2 text-xs text-zinc-500 dark:border-white/5">
            Promote <span className="font-mono font-medium text-zinc-700 dark:text-zinc-200">{snapshot.tag}</span> to…
          </p>
          {done ? (
            <div className="px-3 py-3 text-sm">
              <p role="status">
                {done.promotion.snapshot.tag} is now in <span className="font-medium">{done.label}</span>
                {done.promotion.created ? ", as a new integration." : "."}
              </p>
              <button
                type="button"
                onClick={() => openThere(done.promotion)}
                className="mt-2 text-sm font-medium text-sky-600 hover:underline dark:text-sky-400"
              >
                Open it in {done.label}
              </button>
            </div>
          ) : (
            <ul className="py-1">
              {targets.map((t) => (
                <li key={t.name}>
                  <button
                    type="button"
                    role="menuitem"
                    disabled={working}
                    onClick={() => promote(t)}
                    className="flex w-full items-start gap-2 px-3 py-2 text-left hover:bg-black/[0.04] disabled:opacity-50 dark:hover:bg-white/[0.06]"
                  >
                    <Layers size={14} className="mt-0.5 shrink-0 text-zinc-400" aria-hidden />
                    <span className="min-w-0">
                      <span className="block truncate text-sm font-medium text-zinc-800 dark:text-zinc-100">{t.label}</span>
                      <span className="block truncate font-mono text-[11px] text-zinc-500">
                        {t.name} · {t.namespace}
                      </span>
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          )}
          {error && (
            <div role="alert" className="border-t border-black/5 dark:border-white/5">
              <Callout flush>{error}</Callout>
            </div>
          )}
          <p className="border-t border-black/5 px-3 py-2 text-[11px] text-zinc-400 dark:border-white/5">
            Flows and templates are copied; env files stay the target&rsquo;s own.
          </p>
        </div>
      )}
    </div>
  );
}
