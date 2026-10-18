import { mergeConfigFiles } from "@octo/editor/runtime";
import type { IntegrationRecord } from "./backend";

/**
 * An integration as one definition: its files merged the way the runtime merges a
 * folder. What a run is given, and what every tool that only reads — listing flows,
 * env keys, block addresses — reads, since a name may be declared in any file.
 */
export function definitionOf(rec: IntegrationRecord): string {
  return mergeConfigFiles(rec.files);
}
