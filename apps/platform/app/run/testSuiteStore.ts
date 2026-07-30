import type { TestSuiteStore } from "@octo/editor";
import {
  deleteTestSuite,
  listTestSuites,
  saveTestSuite,
} from "@/app/actions/testSuites";
import { unwrap } from "@/app/model/bff";

/**
 * The platform test-suite store: the integration's `.octo/tests/<flow>_test.yaml`
 * resources in the orchestrator, read and written through the auth-gated actions — the
 * same path the Dev .env panel and the editor-meta store take.
 *
 * Thin on purpose. Which flow a suite tests is read from the suite's own `flow:` key,
 * and that happens in the action rather than here, so this client-reachable module does
 * not pull the editor's runtime helpers into the browser bundle.
 *
 * A resource needs an owning integration, so editing is only available once the
 * integration is saved; until then suites live for the session.
 */
export const bffTestSuiteStore: TestSuiteStore = {
  async list(integrationId) {
    if (!integrationId) return [];
    return unwrap(await listTestSuites(integrationId));
  },
  async save(integrationId, flow, content) {
    if (!integrationId) return; // canEdit gates this; a draft has nothing to own it
    unwrap(await saveTestSuite(integrationId, flow, content));
  },
  async remove(integrationId, flow) {
    if (!integrationId) return;
    unwrap(await deleteTestSuite(integrationId, flow));
  },
  canEdit(integrationId) {
    return !!integrationId;
  },
};
