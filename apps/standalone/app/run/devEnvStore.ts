import type { DevEnvStore } from "@octo/editor";
import { loadDevEnv, saveDevEnv } from "../actions/devEnv";
import { unwrap } from "../actions/result";

/**
 * The dev-env store: reads and writes the `.env.dev` resource through server actions
 * over the local flows dir. One file is shared across flows, so the integration id is
 * ignored and editing works even for an unsaved draft.
 */
export const localDevEnvStore: DevEnvStore = {
  async load() {
    return unwrap(await loadDevEnv());
  },
  async save(_integrationId, content) {
    unwrap(await saveDevEnv(content));
  },
  canEdit() {
    return true;
  },
};
