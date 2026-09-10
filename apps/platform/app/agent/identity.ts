/**
 * Who Dr. Octo is, as a fact rather than as configuration.
 *
 * Its own module, and deliberately dependency-free, because both halves of the
 * app need it: the server-side conversation client keys his memory on it, and a
 * browser component reads it to recognise his integration. Left in the client
 * layer, that second import dragged the whole orchestrator client — and now the
 * session cookie behind it — into the browser bundle.
 */

/**
 * The agent id Dr. Octo declares in his own definition.
 *
 * A constant rather than a lookup because it is part of his definition, not of an
 * install: every Dr. Octo is this agent. An install where someone has edited it is
 * supported, and is also not something the panel can guess at.
 */
export const DR_OCTO_AGENT_ID = "dr-octo";
