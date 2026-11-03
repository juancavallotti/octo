"use client";

import Link from "next/link";
import {
  CircleCheck,
  FlaskConical,
  FlaskConicalOff,
  GitCompareArrows,
  RefreshCw,
} from "lucide-react";
import { useRoles } from "@/app/auth/RolesContext";
import { CAPABILITY_REASONS } from "@/app/auth/capabilities";
import UpgradeRow from "./UpgradeRow";
import { useUpgrades } from "./useUpgrades";

/**
 * Runtime upgrades across the installation: which deployments run an older
 * runtime than this install deploys, whether their suites pass on the new one,
 * and which deployments have no suites to tell.
 */
export default function UpgradesView() {
  const { can } = useRoles();
  const u = useUpgrades();
  const runtime = u.view?.runtimeVersion;
  const testable = u.behind.filter((d) => d.hasSuites);

  return (
    <div className="mx-auto w-full max-w-5xl px-6 py-8">
      <div className="flex items-center gap-2">
        <h1 className="text-xl font-semibold tracking-tight">Upgrades</h1>
        <button
          type="button"
          onClick={() => void u.refresh()}
          aria-label="Refresh upgrades"
          className="ml-1 rounded-md p-1 text-zinc-400 transition-colors hover:bg-black/[0.05] hover:text-zinc-700 dark:hover:bg-white/[0.06] dark:hover:text-zinc-200"
        >
          <RefreshCw size={14} />
        </button>
      </div>
      <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">
        {runtime
          ? `This install deploys 🐙 ${runtime}. Test each deployment's suites on it before rolling over.`
          : "This install does not say which runtime it deploys, so nothing can be compared."}
      </p>

      {u.error && (
        <p className="mt-4 rounded-lg border border-red-500/20 bg-red-500/5 px-3 py-2 text-sm text-red-500">
          {u.error}
        </p>
      )}

      <section className="mt-8">
        <div className="flex items-center gap-2">
          <h2 className="text-sm font-semibold uppercase tracking-wide text-zinc-400">
            Behind
          </h2>
          {u.behind.length > 0 && (
            <span className="rounded-full bg-black/[0.06] px-2 py-0.5 text-xs font-medium text-zinc-500 dark:bg-white/[0.08] dark:text-zinc-400">
              {u.behind.length}
            </span>
          )}
          {testable.length > 1 && (
            <div
              className="ml-auto flex items-center gap-1.5"
              title={can.deploy ? undefined : CAPABILITY_REASONS.deploy}
            >
              <button
                type="button"
                disabled={!can.deploy}
                onClick={() => void u.testAll(false)}
                className="inline-flex items-center gap-1.5 rounded-md border border-black/10 px-2.5 py-1 text-xs font-medium text-zinc-700 hover:bg-black/[0.04] disabled:opacity-50 dark:border-white/10 dark:text-zinc-200 dark:hover:bg-white/[0.06]"
              >
                <FlaskConical size={13} />
                Test all
              </button>
              <button
                type="button"
                disabled={!can.deploy}
                title="Test every deployment with suites, and upgrade each one whose suites pass."
                onClick={() => void u.testAll(true)}
                className="inline-flex items-center gap-1.5 rounded-md bg-sky-600 px-2.5 py-1 text-xs font-medium text-white hover:bg-sky-500 disabled:opacity-50"
              >
                <GitCompareArrows size={13} />
                Test &amp; upgrade all
              </button>
            </div>
          )}
        </div>
        {u.view === null ? (
          <p className="mt-4 text-sm text-zinc-400">Loading…</p>
        ) : u.behind.length === 0 ? (
          <p className="mt-4 inline-flex items-center gap-2 text-sm text-emerald-700 dark:text-emerald-400">
            <CircleCheck size={15} />
            Every deployment runs this install&apos;s runtime.
          </p>
        ) : (
          <ul className="mt-4 flex flex-col gap-2">
            {u.behind.map((d) => (
              <UpgradeRow
                key={d.deploymentId}
                d={d}
                runtime={runtime}
                busy={u.pending.has(d.deploymentId)}
                canDeploy={can.deploy}
                deniedReason={CAPABILITY_REASONS.deploy}
                onTest={(rolloutOnPass) => void u.test(d, rolloutOnPass)}
                onUpgrade={() => void u.upgrade(d)}
              />
            ))}
          </ul>
        )}
      </section>

      {u.untested.length > 0 && (
        <section className="mt-10">
          <div className="flex items-center gap-2">
            <FlaskConicalOff
              size={14}
              className="text-amber-600 dark:text-amber-400"
            />
            <h2 className="text-sm font-semibold uppercase tracking-wide text-zinc-400">
              No tests
            </h2>
            <span className="rounded-full bg-amber-500/10 px-2 py-0.5 text-xs font-medium text-amber-700 dark:text-amber-400">
              {u.untested.length}
            </span>
          </div>
          <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">
            These deployments run a version with no test suites, so an upgrade
            cannot be checked before it ships. Add suites in the editor&apos;s
            Testing view, tag a new version and roll it out.
          </p>
          <ul className="mt-3 flex flex-wrap gap-1.5">
            {u.untested.map((d) => (
              <li key={d.deploymentId}>
                <Link
                  href={`/platform/integrations/i/${encodeURIComponent(d.integrationId)}`}
                  className="inline-flex items-center gap-1.5 rounded-full border border-amber-500/30 px-2 py-0.5 text-xs text-zinc-700 hover:bg-amber-500/10 dark:text-zinc-200"
                >
                  <span className="max-w-[14rem] truncate">
                    {d.integrationName}
                  </span>
                  {d.tag && (
                    <span className="font-mono text-[10px] text-zinc-500">
                      {d.tag}
                    </span>
                  )}
                </Link>
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}
