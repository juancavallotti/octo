import type { TestSuiteStore } from "@octo/editor";
import {
  deleteTestSuite,
  listTestSuites,
  saveTestSuite,
} from "../actions/testSuites";
import { unwrap } from "../actions/result";

/**
 * The standalone test-suite store: `<flow>_test.yaml` files under the flows directory,
 * shared by every document there the way `.env.dev` is — which is why the integration id
 * is not passed to the actions.
 *
 * Editing needs a saved document. Not because the storage is keyed by it, but because a
 * suite that cannot be committed beside a flow file that does not exist yet is not the
 * artifact the tab promises; until then suites live for the session.
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
