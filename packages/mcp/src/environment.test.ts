import { describe, expect, it } from "vitest";
import { Client, StreamableHTTPClientTransport } from "@modelcontextprotocol/client";
import { createOctoMcpHandler } from "./handler";
import type { EnvironmentScope, OctoMcpConfig } from "./backend";
import type { RunHostPort } from "./run-host";

const stubRunHost = {
  binaries: () => ({ available: false, version: null }),
  start: async () => ({ running: false, exposable: false, port: null, testUrl: null }),
  stop: async () => ({ running: false, exposable: false, port: null, testUrl: null }),
  invoke: async () => ({ ok: false, exitCode: null, timedOut: false, dropped: false, output: "", logs: [] }),
  test: async () => ({ ok: false, exitCode: null, timedOut: false, totals: {}, suites: [], logs: [] }),
  evalCel: async () => ({ ok: false, error: "unavailable", logs: [] }),
  snapshot: async () => [],
  newNamespace: () => "ns-1",
} as unknown as RunHostPort;

/**
 * A host with two environments, whose store answers with the environment it
 * was called in: what a tool sees is exactly what the host's scope made of the
 * argument.
 */
function hostWithEnvironments() {
  let active = "";
  const calls: string[] = [];
  const environments: EnvironmentScope = {
    list: async () => [
      { name: "octo", displayName: "Default", description: "", default: true },
      { name: "staging", displayName: "Staging", description: "", default: false },
    ],
    run: async (environment, fn) => {
      const before = active;
      active = environment;
      try {
        return await fn();
      } finally {
        active = before;
      }
    },
  };
  const config: OctoMcpConfig = {
    store: {
      list: async () => {
        calls.push(active);
        return [{ id: `in-${active || "default"}`, name: "orders", files: [] }];
      },
      get: async () => ({ id: "", name: "", files: [] }),
      create: async () => ({ id: "", name: "", files: [] }),
      update: async () => ({ id: "", name: "", files: [] }),
    },
    validate: () => ({ valid: true, errors: [] }),
    runtimeSchema: {},
    environments,
  };
  return { config, calls };
}

async function connect(config: OctoMcpConfig): Promise<Client> {
  const handler = createOctoMcpHandler(config, { runHost: stubRunHost });
  const client = new Client({ name: "t", version: "0" });
  await client.connect(
    new StreamableHTTPClientTransport(new URL("http://localhost/mcp"), {
      fetch: (input, init) => handler(new Request(input, init)),
    }),
  );
  return client;
}

function text(result: unknown): string {
  return (result as { content: { text: string }[] }).content[0].text;
}

describe("environments", () => {
  it("lists them, and offers the argument on the tools that touch the host's data", async () => {
    const client = await connect(hostWithEnvironments().config);
    const { tools } = await client.listTools();
    const byName = new Map(tools.map((t) => [t.name, t]));

    expect(byName.has("list_environments")).toBe(true);
    const properties = (t: string) => Object.keys((byName.get(t)?.inputSchema as { properties?: object }).properties ?? {});
    expect(properties("list_integrations")).toContain("environment");
    expect(properties("evaluate_cel")).not.toContain("environment");

    const listed = JSON.parse(text(await client.callTool({ name: "list_environments", arguments: {} })));
    expect(listed.map((e: { name: string }) => e.name)).toEqual(["octo", "staging"]);
    await client.close();
  });

  it("runs a tool in the environment it is given, and in the default without one", async () => {
    const { config, calls } = hostWithEnvironments();
    const client = await connect(config);

    const inStaging = text(await client.callTool({ name: "list_integrations", arguments: { environment: "staging" } }));
    const inDefault = text(await client.callTool({ name: "list_integrations", arguments: {} }));

    expect(inStaging).toContain("in-staging");
    expect(inDefault).toContain("in-default");
    expect(calls).toEqual(["staging", ""]);
    await client.close();
  });

  it("adds nothing for a host without environments", async () => {
    const { config } = hostWithEnvironments();
    const client = await connect({ ...config, environments: undefined });
    const { tools } = await client.listTools();
    expect(tools.some((t) => t.name === "list_environments")).toBe(false);
    const list = tools.find((t) => t.name === "list_integrations");
    expect(Object.keys((list?.inputSchema as { properties?: object }).properties ?? {})).not.toContain("environment");
    await client.close();
  });
});
