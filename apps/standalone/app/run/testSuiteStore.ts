import type { TestSuiteStore } from "@octo/editor";
import {
  deleteTestSuite,
  listTestSuites,
  saveTestSuite,
} from "../actions/testSuites";
import { unwrap } from "../actions/result";

/**
 * The test-suite store: `<flow>_test.yaml` files under the flows directory, shared by
 * every document there, so the integration id never reaches the actions.
 *
 * Editing needs a saved document — not because the storage is keyed by it, but because
 * a suite is meant to sit beside a flow file that exists. Until then suites live for
 * the session.
 */
export const localTestSuiteStore: TestSuiteStore = {
  async list() {
    return unwrap(await listTestSuites());
  },
  async save(_integrationId, flow, content) {
    unwrap(await saveTestSuite(flow, content));
  },
  async remove(_integrationId, flow) {
    unwrap(await deleteTestSuite(flow));
  },
  canEdit(integrationId) {
    return !!integrationId;
  },
};
