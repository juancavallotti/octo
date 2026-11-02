"use client";

import { useEffect, useState } from "react";
import {
  loadIntegrationDetail,
  type IntegrationDetailData,
} from "@/app/model/orchestrator";

const MISSING: IntegrationDetailData = {
  snapshots: null,
  deployments: null,
  resources: null,
  envVars: null,
};

/**
 * The detail pane's first view, read once when an integration is opened. Each
 * section is handed its part and skips its own first read; after that the
 * sections reload themselves, as they always have.
 *
 * `null` until the read settles. A failed read settles to every part missing,
 * which sends each section back to reading its own.
 */
export function useIntegrationDetail(id: string): IntegrationDetailData | null {
  const [read, setRead] = useState<{ id: string; data: IntegrationDetailData } | null>(
    null,
  );

  useEffect(() => {
    let live = true;
    loadIntegrationDetail(id).then(
      (data) => live && setRead({ id, data }),
      () => live && setRead({ id, data: MISSING }),
    );
    return () => {
      live = false;
    };
  }, [id]);

  return read?.id === id ? read.data : null;
}
