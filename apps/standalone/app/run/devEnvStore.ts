import type { DevEnvStore } from "@octo/editor";
import { loadDevEnv, saveDevEnv } from "../actions/devEnv";
import { unwrap } from "../actions/result";

/**
 * The standalone dev-env store: the editor's Dev .env panel reads and writes the
 * `.env.dev` resource through server actions backed by the local flows dir. The
 * file is shared across flows (standalone has no per-integration partition), so
 * the integration id is ignored and editing is always available — including for
 * an unsaved draft.
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
