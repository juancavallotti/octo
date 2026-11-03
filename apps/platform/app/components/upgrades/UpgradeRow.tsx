"use client";

import { useState } from "react";
import Link from "next/link";
import {
  ArrowRight,
  ChevronDown,
  ChevronRight,
  FlaskConical,
  GitCompareArrows,
  Pin,
} from "lucide-react";
import type { NamedDeploymentUpgrade } from "@/app/model/upgrades";
import TestRunReport, { StatusIcon } from "./TestRunReport";
import { RUN_TONE, currentRun, runSummary } from "./runVerdict";
import {
  RuntimePill,
  VersionPill,
} from "@/app/components/integrations/DeploymentRowParts";

const BUTTON =
  "inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium transition-colors disabled:opacity-50";

/** One deployment behind this install's runtime, with what can be done about it. */
export default function UpgradeRow({
  d,
  runtime,
  busy,
  canDeploy,
  deniedReason,
  onTest,
  onUpgrade,
}: {
  d: NamedDeploymentUpgrade;
  /** The runtime release this install deploys. */
  runtime?: string;
  busy: boolean;
  canDeploy: boolean;
  deniedReason?: string;
  onTest: (rolloutOnPass: boolean) => void;
  onUpgrade: () => void;
}) {
  const [open, setOpen] = useState(false);
  const run = currentRun(d, runtime);
  const testing = run?.status === "running";
  const disabled = busy || testing || !canDeploy;

  return (
    <li className="rounded-lg border border-black/10 bg-white/40 dark:border-white/10 dark:bg-zinc-900/30">
      <div className="flex flex-wrap items-center gap-3 px-4 py-3">
        <div className="min-w-0 flex-1">
          <Link
            href={`/platform/integrations/i/${encodeURIComponent(d.integrationId)}`}
            className="truncate text-sm font-semibold hover:underline"
          >
            {d.integrationName}
          </Link>
          <div className="mt-0.5 flex flex-wrap items-center gap-2 text-xs text-zinc-500 dark:text-zinc-400">
            {d.tag && <VersionPill tag={d.tag} />}
            {d.pinned && (
              <span className="inline-flex items-center gap-1 rounded-full bg-amber-500/15 px-2 py-0.5 font-medium text-amber-700 dark:text-amber-400">
                <Pin size={10} />
                Pinned
              </span>
            )}
            <span className="inline-flex items-center gap-1">
              <RuntimePill
                version={d.runtimeVersion ?? "unknown"}
                behind={d.upgradeTo}
              />
              <ArrowRight size={11} />
              {d.upgradeTo && <RuntimePill version={d.upgradeTo} target />}
            </span>
          </div>
        </div>

        <div className="flex items-center gap-2 text-xs">
          {!d.hasSuites ? (
            <span className="rounded-full bg-amber-500/10 px-2 py-0.5 font-medium text-amber-700 dark:text-amber-400">
              No tests
            </span>
          ) : run ? (
            <button
              type="button"
              onClick={() => setOpen((o) => !o)}
              className={`inline-flex items-center gap-1 font-medium ${RUN_TONE[run.status]}`}
            >
              <StatusIcon status={run.status} size={13} />
              {runSummary(run)}
              {open ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
            </button>
          ) : (
            <span className="text-zinc-500 dark:text-zinc-400">
              {d.latestRunStale && d.latestRun?.runtimeVersion === runtime
                ? "Not tested since the last rollout"
                : `Not tested on ${runtime}`}
            </span>
          )}
        </div>

        <div
          className="flex items-center gap-1.5"
          title={canDeploy ? undefined : deniedReason}
        >
          {d.pinned ? (
            <>
              {d.hasSuites && (
                <button
                  type="button"
                  disabled={disabled}
                  onClick={() => onTest(false)}
                  className={`${BUTTON} border border-black/10 text-zinc-700 hover:bg-black/[0.04] dark:border-white/10 dark:text-zinc-200 dark:hover:bg-white/[0.06]`}
                >
                  <FlaskConical size={13} />
                  Test
                </button>
              )}
              <button
                type="button"
                disabled={disabled}
                onClick={onUpgrade}
                title="Pinned deployments are upgraded by hand: this moves the pin, without testing."
                className={`${BUTTON} border border-black/10 text-zinc-700 hover:bg-black/[0.04] dark:border-white/10 dark:text-zinc-200 dark:hover:bg-white/[0.06]`}
              >
                <Pin size={13} />
                Move pin to {d.upgradeTo}
              </button>
            </>
          ) : d.hasSuites ? (
            <>
              <button
                type="button"
                disabled={disabled}
                onClick={() => onTest(false)}
                className={`${BUTTON} border border-black/10 text-zinc-700 hover:bg-black/[0.04] dark:border-white/10 dark:text-zinc-200 dark:hover:bg-white/[0.06]`}
              >
                <FlaskConical size={13} />
                Test
              </button>
              <button
                type="button"
                disabled={disabled}
                onClick={() => onTest(true)}
                className={`${BUTTON} bg-sky-600 text-white hover:bg-sky-500`}
              >
                <GitCompareArrows size={13} />
                Test &amp; upgrade
              </button>
            </>
          ) : (
            <button
              type="button"
              disabled={disabled || !d.snapshotId}
              onClick={onUpgrade}
              title={
                d.snapshotId
                  ? "Rolls out without testing: this version has no test suites."
                  : "It runs no version tag, so there is nothing to roll out. Deploy a tagged version first."
              }
              className={`${BUTTON} border border-amber-500/40 text-amber-700 hover:bg-amber-500/10 dark:text-amber-400`}
            >
              <GitCompareArrows size={13} />
              Upgrade untested
            </button>
          )}
        </div>
      </div>
      {open && run && (
        <div className="border-t border-black/10 px-4 py-3 dark:border-white/10">
          <TestRunReport
            run={run}
            editorHref={`/platform/i/${encodeURIComponent(d.integrationId)}`}
          />
        </div>
      )}
    </li>
  );
}
