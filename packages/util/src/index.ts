/**
 * @octo/util — small, dependency-free helpers shared by the apps.
 *
 * Nothing here knows about a service, a document, or a framework. A helper earns
 * a place in this package by being useful to more than one app *and* by being
 * pure enough to test on its own; anything that needs a fetch, a schema, or a
 * React tree belongs to the app or the package that owns that concern.
 */

export type { FilterRankedOptions, RankBias } from "./search";
export { filterRanked, rankSearchString } from "./search";
