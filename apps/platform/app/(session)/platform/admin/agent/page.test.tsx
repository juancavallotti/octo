import { describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";

vi.mock("@/app/model/agent", () => ({
  getAgentStatus: () =>
    Promise.resolve({
      state: "not_installed",
      updateAvailable: false,
      edited: false,
      tracing: false,
      blocked: "llm_key",
    }),
  installAgent: () => Promise.resolve(),
  rolloutAgent: () => Promise.resolve(),
  setAgentTracing: () => Promise.resolve(),
  setAgentMaxIterations: () => Promise.resolve(),
  uninstallAgent: () => Promise.resolve(),
}));

vi.mock("@/app/model/siteSettings", () => ({
  getLlmSettings: () =>
    Promise.resolve({
      provider: "ANTHROPIC",
      model: "claude-sonnet-4-6",
      configured: false,
      last4: "",
      updatedAt: "",
      encryptionAvailable: true,
    }),
  saveLlmSettings: () => Promise.resolve(),
  getWebSearchSettings: () =>
    Promise.resolve({
      provider: "PARALLEL",
      configured: false,
      last4: "",
      updatedAt: null,
      encryptionAvailable: true,
    }),
  saveWebSearchSettings: () => Promise.resolve(),
}));

import AdminAgentPage from "./page";

/**
 * These were two tabs for one task, and this is what holds them together now.
 *
 * The assertions are about *composition* rather than behaviour — each manager has
 * its own suite for that. What can regress here is somebody moving one back out to
 * its own route, which would restore the exact split this page exists to undo: the
 * page that refuses to install the agent would once again not be the page holding
 * the key he is refused for.
 *
 * Web search sits between them for the same reason the other two are here at all:
 * it is a key this agent uses, so it belongs on the page that installs him — and it
 * goes below the provider because he runs without it and does not run without that.
 *
 * The absence of an embedding section is asserted too, because it was here once and
 * putting it back is the tempting mistake. Nothing on this platform configures
 * embeddings — the provider, model and key are chart values on the embedding server
 * — so what would land here is a status report about searching agent memory, which
 * belongs on the page that searches it.
 */
describe("the platform agent page", () => {
  it("carries the provider and the deployment on one page", async () => {
    render(<AdminAgentPage />);

    expect(
      screen.getByRole("heading", { name: "Platform agent" }),
    ).toBeTruthy();
    expect(screen.getByRole("heading", { name: "LLM provider" })).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Web search" })).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Deployment" })).toBeTruthy();
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Install" })).toBeTruthy(),
    );
  });

  it("says nothing about embeddings, which are not configured here", () => {
    render(<AdminAgentPage />);

    expect(screen.queryByRole("heading", { name: /Embeddings/ })).toBeNull();
    expect(screen.queryByRole("progressbar")).toBeNull();
  });

  // The whole point of the merge: the requirement and the field it names are now
  // close enough that the link is an anchor rather than a navigation.
  it("points a blocked install at the provider section above it", async () => {
    render(<AdminAgentPage />);

    await waitFor(() =>
      expect(
        (screen.getByRole("button", { name: "Install" }) as HTMLButtonElement)
          .disabled,
      ).toBe(true),
    );
    expect(
      screen
        .getByRole("link", { name: /Configure the LLM provider/ })
        .getAttribute("href"),
    ).toBe("#llm-heading");
    expect(document.getElementById("llm-heading")).toBeTruthy();
  });
});
