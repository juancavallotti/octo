/**
 * The line that says how search on this page ranks.
 *
 * It reports; it does not configure. That distinction is why it is here and not
 * under Admin, where it started life as a provider/model/key form — and these
 * assert the part that survived the move: an answer, and no progress bar.
 */

import { describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";

const settings = vi.hoisted(() => ({ getEmbeddingStatus: vi.fn() }));
vi.mock("@/app/model/siteSettings", () => settings);

import { SearchRanking } from "./SearchRanking";

describe("the search ranking line", () => {
  it("names the model when search ranks by meaning", async () => {
    settings.getEmbeddingStatus.mockResolvedValue({
      configured: true,
      reachable: true,
      model: "text-embedding-3-small",
      pending: 0,
    });
    render(<SearchRanking />);

    await waitFor(() => expect(screen.getByText(/ranks by meaning/)).toBeTruthy());
    expect(screen.getByText("text-embedding-3-small")).toBeTruthy();
    // Nothing outstanding is the ordinary state and says nothing about a backfill.
    expect(screen.queryByText(/waiting for a vector/)).toBeNull();
    expect(screen.queryByRole("progressbar")).toBeNull();
  });

  // The reason the count is here at all: configuring a provider does not make
  // search semantic, it makes it become semantic, and an operator who searches
  // during that and finds nothing deserves to know why.
  it("says what is still waiting while a backfill drains", async () => {
    settings.getEmbeddingStatus.mockResolvedValue({
      configured: true,
      reachable: true,
      model: "text-embedding-3-small",
      pending: 12,
    });
    render(<SearchRanking />);

    await waitFor(() => expect(screen.getByText(/12 stored items are/)).toBeTruthy());
  });

  // Not a warning, and not styled as one. Running without an embedding server is
  // supported; colouring it as a fault sends someone looking for one.
  it("states plainly that search matches text without a server", async () => {
    settings.getEmbeddingStatus.mockResolvedValue({
      configured: false,
      reachable: false,
      pending: 0,
    });
    render(<SearchRanking />);

    await waitFor(() => expect(screen.getByText(/Search matches text/)).toBeTruthy());
  });

  // A status probe that fails is not this page's problem to shout about: the
  // conversations beside it loaded fine, and a red error about something nobody
  // asked for is worse than saying nothing.
  it("says nothing at all when the probe fails", async () => {
    settings.getEmbeddingStatus.mockRejectedValue(new Error("nope"));
    const { container } = render(<SearchRanking />);

    await waitFor(() => expect(container.textContent).toBe(""));
  });
});
