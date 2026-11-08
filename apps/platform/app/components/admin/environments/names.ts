import { DEFAULT_ENVIRONMENT } from "@/app/lib/environment";

/** Mirrors the orchestrator: a DNS label, so it can become part of a namespace. */
const NAME_RE = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;
const MAX_NAME = 30;

/**
 * Why `name` cannot name an environment, or null when it can. The orchestrator
 * checks the same rule; saying so here spares a round trip that would only come
 * back with the same sentence.
 */
export function nameProblem(name: string): string | null {
  if (name === "") return null;
  if (name === DEFAULT_ENVIRONMENT) return "“octo” is the default environment.";
  if (name.length > MAX_NAME) return `At most ${MAX_NAME} characters.`;
  if (!NAME_RE.test(name)) {
    return "Lowercase letters, digits and hyphens, starting and ending with a letter or digit.";
  }
  return null;
}

/** The namespace the orchestrator will create when none is given. */
export function defaultNamespace(name: string): string {
  return name ? `octo-${name}` : "octo-…";
}
