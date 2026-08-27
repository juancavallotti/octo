/**
 * The embedding settings form. Two behaviours are load-bearing and neither is
 * obvious from the markup: the backfill progress, and the confirmation before a
 * model change that cannot be undone.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const getEmbeddingSettings = vi.fn();
const saveEmbeddingSettings = vi.fn();
const clearEmbeddingSettings = vi.fn();
const confirm = vi.fn();

vi.mock("@/app/model/siteSettings", () => ({
  getEmbeddingSettings: () => getEmbeddingSettings(),
  saveEmbeddingSettings: (input: unknown) => saveEmbeddingSettings(input),
  clearEmbeddingSettings: () => clearEmbeddingSettings(),
}));

vi.mock("@/app/components/ConfirmDialog", () => ({
  useConfirm: () => confirm,
}));

import EmbeddingSettingsManager from "./EmbeddingSettingsManager";

function status(over: Record<string, unknown> = {}) {
  return {
    settings: {
      provider: "OPENAI",
      model: "text-embedding-3-small",
      dimensions: 1536,
      configured: true,
      last4: "1234",
      updatedAt: null,
    },
    embedded: 0,
    pending: 0,
    encryptionAvailable: true,
    ...over,
  };
}

beforeEach(() => {
  getEmbeddingSettings.mockResolvedValue(status());
  saveEmbeddingSettings.mockResolvedValue(status().settings);
  clearEmbeddingSettings.mockResolvedValue(undefined);
  confirm.mockResolvedValue(true);
});

afterEach(() => {
  vi.clearAllMocks();
});

describe("EmbeddingSettingsManager", () => {
  it("does not offer Anthropic, which has no embeddings API", async () => {
    render(<EmbeddingSettingsManager />);
    await screen.findByLabelText(/^Provider/);
    expect(screen.queryByRole("option", { name: "Anthropic" })).toBeNull();
    expect(screen.getByRole("option", { name: "OpenAI" })).toBeTruthy();
  });

  // Configuring a provider does not make search semantic — it makes it become
  // semantic, over however long the backlog takes. Without this an operator who
  // configures and searches immediately concludes it did not work.
  it("shows how far the backfill has got", async () => {
    getEmbeddingSettings.mockResolvedValue(status({ embedded: 40, pending: 60 }));
    render(<EmbeddingSettingsManager />);
    await screen.findByText(/40 of 100 vectorized/);
    expect(screen.getByRole("progressbar").getAttribute("aria-valuenow")).toBe("40");
  });

  it("says so when everything is vectorized", async () => {
    getEmbeddingSettings.mockResolvedValue(status({ embedded: 12, pending: 0 }));
    render(<EmbeddingSettingsManager />);
    await screen.findByText(/All 12 stored items are vectorized/);
  });

  // The one setting here a second change cannot undo: vectors carry no record of
  // which model produced them, and the platform does not re-embed.
  it("asks before changing the model when something is already embedded", async () => {
    getEmbeddingSettings.mockResolvedValue(status({ embedded: 500 }));
    render(<EmbeddingSettingsManager />);
    const model = await screen.findByLabelText(/^Model/);

    await userEvent.clear(model);
    await userEvent.type(model, "text-embedding-3-large");
    await userEvent.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => expect(confirm).toHaveBeenCalled());
    expect(confirm.mock.calls[0][0].body).toContain("500");
  });

  it("does not ask when nothing has been embedded yet", async () => {
    render(<EmbeddingSettingsManager />);
    const model = await screen.findByLabelText(/^Model/);

    await userEvent.clear(model);
    await userEvent.type(model, "text-embedding-3-large");
    await userEvent.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => expect(saveEmbeddingSettings).toHaveBeenCalled());
    expect(confirm).not.toHaveBeenCalled();
  });

  it("saves without the key when the field is left alone, so the credential survives", async () => {
    render(<EmbeddingSettingsManager />);
    await screen.findByLabelText(/^Model/);

    await userEvent.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => expect(saveEmbeddingSettings).toHaveBeenCalled());
    expect(saveEmbeddingSettings.mock.calls[0][0]).not.toHaveProperty("apiKey");
  });
});
