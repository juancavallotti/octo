// @vitest-environment node
import { describe, it, expect } from "vitest";
import { createProtectedResourceGET } from "./protected-resource";

const config = {
  issuer: "https://idp.example.com",
  resource: "https://platform.example/mcp",
  scopes: "openid  profile email",
};

const get = (handler: (request: Request) => Response) =>
  handler(new Request("https://platform.example/.well-known/oauth-protected-resource/mcp"));

describe("createProtectedResourceGET", () => {
  it("names the sign-in scopes, so a client asks for a token iam can resolve", async () => {
    const res = get(createProtectedResourceGET(config));
    expect(res.status).toBe(200);
    expect(await res.json()).toMatchObject({
      resource: "https://platform.example/mcp",
      authorization_servers: ["https://idp.example.com"],
      scopes_supported: ["openid", "profile", "email"],
    });
  });

  it("answers 404 when there is no issuer or no public origin to advertise", () => {
    expect(get(createProtectedResourceGET({ ...config, issuer: "" })).status).toBe(404);
    expect(get(createProtectedResourceGET({ ...config, resource: "" })).status).toBe(404);
  });
});
