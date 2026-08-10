"use client";

import { useEffect, useRef, useState } from "react";

import { getDeployOptions, type DeployOptions, type Snapshot } from "@/app/model/orchestrator";

/**
 * Load the deploy options for a version: a tag reads its frozen definition,
 * `null` the live working copy. They drive networked-ness, a free slug to
 * prefill, and the env vars to prompt for.
 *
 * Both the deploy and rollout dialogs need exactly this, and had it twice. The
 * two copies had already drifted in one respect — one prefilled a slug from the
 * suggestion, the other did not — so `onLoaded` keeps that difference explicit
 * instead of averaging it away.
 *
 * On failure it settles on non-networked with no declared env rather than staying
 * null: the dialog still has to let the operator through for a plain version
 * bump. `onLoaded` is deliberately not called there — the fallback is an
 * assumption, not an answer, and prefilling from an assumption would put a slug
 * in the box that nothing suggested.
 */
export function useDeployOptions(
  integrationId: string,
  snapshot: Snapshot | null,
  onLoaded?: (opts: DeployOptions) => void,
): DeployOptions | null {
  const [opts, setOpts] = useState<DeployOptions | null>(null);

  // Held in a ref so an inline callback — which every caller writes — does not
  // change identity each render and re-run the fetch below. Assigned in an effect
  // rather than during render, which React forbids: under concurrent rendering a
  // render may be discarded, and a ref written from one is a mutation that
  // survives work that never happened.
  const loaded = useRef(onLoaded);
  useEffect(() => {
    loaded.current = onLoaded;
  }, [onLoaded]);

  useEffect(() => {
    let active = true;
    getDeployOptions(
      integrationId,
      snapshot ? { snapshotId: snapshot.id } : {},
    ).then(
      (o) => {
        if (!active) return;
        setOpts(o);
        loaded.current?.(o);
      },
      () =>
        active &&
        setOpts({ networked: false, slugValid: false, slugAvailable: false }),
    );
    return () => {
      active = false;
    };
  }, [integrationId, snapshot]);

  return opts;
}
