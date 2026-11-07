import { useState } from "react";

/**
 * The environment the run on screen acts in: the one it started in while he is
 * working, whatever page he has since taken the user to, and the current one
 * once he is done.
 */
export function useRunEnvironment(busy: boolean, environment: string): string {
  const [started, setStarted] = useState(environment);
  if (!busy && started !== environment) setStarted(environment);
  return busy ? started : environment;
}

/**
 * Why a message must wait for the run in flight to end rather than join it, or
 * null when it may be sent now.
 *
 * Files: the composer refuses to attach while he is working, but `busy` can also
 * turn true from another tab writing into this conversation, so files may already
 * be here when a steer is all this send can be. Kept, never sent to be dropped:
 * see useAgentChat's steer branch and #516.
 *
 * Another environment: joined to the run, the message would be answered by tools
 * still acting in the environment the run started in, while saying it is about
 * this one.
 */
export function heldBack(
  busy: boolean,
  hasFiles: boolean,
  runEnvironment: string,
  environment: string,
): string | null {
  if (!busy) return null;
  if (hasFiles) return "He is working. Send the files once he has answered.";
  if (runEnvironment !== environment) {
    return "He is still finishing in the environment he started in. Send this once he has answered.";
  }
  return null;
}
