import { z } from "zod";
import type { McpServer } from "@modelcontextprotocol/server";
import type { EnvironmentScope } from "./backend";
import { guard, jsonResult } from "./result";

const ENVIRONMENT_DESCRIPTION =
  "The environment to act in, by name, as list_environments reports it. Omit for the default environment. " +
  "Integration ids belong to one environment: pass the same environment for every call about the same integration.";

/**
 * A view of `server` whose every registered tool takes an optional `environment`
 * argument and runs in it. Wrapping registration rather than each tool keeps the
 * tools themselves unaware of environments: a tool reads and writes through the
 * host's stores, and the host's scope decides which environment those reach.
 *
 * Tools named in `unscoped` are registered unchanged — the ones that read nothing
 * of the host's, where the argument would be a choice that changes nothing.
 */
export function inEnvironments(
  server: McpServer,
  environments: EnvironmentScope | undefined,
  unscoped: ReadonlySet<string> = new Set(),
): McpServer {
  if (!environments) return server;
  return new Proxy(server, {
    get(target, prop, receiver) {
      if (prop !== "registerTool") return Reflect.get(target, prop, receiver);
      return (name: string, config: { inputSchema?: unknown }, handler: (...args: unknown[]) => unknown) => {
        if (unscoped.has(name) || !(config.inputSchema instanceof z.ZodObject)) {
          return target.registerTool(name, config as never, handler as never);
        }
        const inputSchema = config.inputSchema.extend({
          environment: z.string().optional().describe(ENVIRONMENT_DESCRIPTION),
        });
        const scoped = (args: Record<string, unknown>, ...rest: unknown[]) => {
          const { environment, ...own } = args;
          const name = typeof environment === "string" ? environment.trim() : "";
          return environments.run(name, async () => handler(own, ...rest));
        };
        return target.registerTool(name, { ...config, inputSchema } as never, scoped as never);
      };
    },
  });
}

/** `list_environments`: the names every other tool's `environment` argument takes. */
export function registerEnvironmentTools(server: McpServer, environments: EnvironmentScope | undefined): void {
  if (!environments) return;
  server.registerTool(
    "list_environments",
    {
      title: "List environments",
      description:
        "List the platform's environments as { name, displayName, description, default }. Each is an isolated slice with " +
        "its own integrations, resources, tests and runs. Pass a name as the `environment` argument of any other tool to " +
        "act there; omitting it acts in the default one (default: true).",
      inputSchema: z.object({}),
    },
    () => guard(async () => jsonResult(await environments.list())),
  );
}
