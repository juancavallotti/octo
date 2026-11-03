"use client";

import { useLoadable, type Loadable } from "@/app/components/ui/loadable";
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
 * It never fails as a whole: a failed read settles to every part missing, which
 * sends each section back to reading its own.
 */
export function useIntegrationDetail(id: string): Loadable<IntegrationDetailData> {
  return useLoadable(id, () => loadIntegrationDetail(id).catch(() => MISSING)).state;
}
