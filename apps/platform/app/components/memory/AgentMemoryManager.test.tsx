/**
 * The agent memory viewer. What is asserted here is mostly what the viewer
 * deliberately does NOT do — there is no way to edit a remembered fact, and
 * nothing destructive happens without asking.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

// Declared through vi.hoisted because vi.mock is lifted above the file's own
// statements: a factory closing over an ordinary const reads it before it exists.
const model = vi.hoisted(() => ({
  listMemoryIntegrations: vi.fn(),
  listMemoryAgents: vi.fn(),
  listMemoryThreads: vi.fn(),
  readMemoryThread: vi.fn(),
  readMemoryWorking: vi.fn(),
  deleteMemoryThread: vi.fn(),
  listAgentUserMemories: vi.fn(),
  deleteAgentUserMemory: vi.fn(),
  searchAgentMemory: vi.fn(),
}));
const confirm = vi.hoisted(() => vi.fn());

vi.mock("@/app/model/agentMemory", () => model);
// The ranking line asks the orchestrator whether search is semantic. Mocked
// rather than left to resolve: the real module reaches a server action, and
// under jsdom that pulls next-auth in and the suite never gets as far as a test.
vi.mock("@/app/model/siteSettings", () => ({
  getEmbeddingStatus: () => Promise.resolve({ configured: false, reachable: false, pending: 0 }),
}));
vi.mock("@/app/components/ConfirmDialog", () => ({ useConfirm: () => confirm }));

import AgentMemoryManager from "./AgentMemoryManager";

/** Pick the integration and agent, which everything else is gated behind. */
async function choose() {
  render(<AgentMemoryManager />);
  const integration = await screen.findByLabelText(/^Integration/);
  await userEvent.selectOptions(integration, "int-1");
  const agent = await screen.findByLabelText(/^Agent/);
  await waitFor(() => expect(screen.getByRole("option", { name: /dr-octo/ })).toBeTruthy());
  await userEvent.selectOptions(agent, "dr-octo");
}

beforeEach(() => {
  confirm.mockResolvedValue(true);
  model.listMemoryIntegrations.mockResolvedValue([{ id: "int-1", name: "Dr. Octo" }]);
  model.listMemoryAgents.mockResolvedValue([
    { agentId: "dr-octo", threadCount: 2, lastActivityAt: "2026-08-02T00:00:00Z" },
  ]);
  model.listMemoryThreads.mockResolvedValue({
    threads: [
      {
        agentId: "dr-octo",
        threadKey: "t-1",
        userId: "u-1",
        title: "Deploying",
        version: 1,
        turnCount: 2,
        createdAt: "2026-08-01T00:00:00Z",
        lastActivityAt: "2026-08-02T00:00:00Z",
      },
    ],
  });
  model.readMemoryThread.mockResolvedValue({
    thread: { threadKey: "t-1", title: "Deploying", userId: "u-1", turnCount: 2 },
    turns: [
      { seq: 1, role: "user", text: "how do I deploy?", createdAt: "2026-08-01T00:00:00Z" },
      { seq: 2, role: "assistant", text: "roll it out", createdAt: "2026-08-01T00:00:01Z" },
    ],
  });
  // Compacted relative to the transcript above — one message where two turns were
  // said — because that gap is what the panel exists to make visible.
  model.readMemoryWorking.mockResolvedValue({
    found: true,
    version: 3,
    iteration: 2,
    tokens: 1200,
    updatedAt: "2026-08-01T00:00:01Z",
    bytes: 2048,
    readable: true,
    payload: JSON.stringify({ v: 1, messages: [{ Role: "user", Text: "how do I deploy?" }] }),
  });
  model.listAgentUserMemories.mockResolvedValue([
    {
      name: "prefers-go",
      value: "Prefers Go examples.",
      version: 1,
      createdAt: "2026-08-01T00:00:00Z",
      updatedAt: "2026-08-01T00:00:00Z",
    },
  ]);
  model.deleteMemoryThread.mockResolvedValue(undefined);
  model.deleteAgentUserMemory.mockResolvedValue(undefined);
});

afterEach(() => vi.clearAllMocks());

describe("AgentMemoryManager", () => {
  it("lists an agent's conversations once one is chosen", async () => {
    await choose();
    await screen.findByText("Deploying");
    expect(screen.getByText(/2 turns/)).toBeTruthy();
  });

  it("shows a conversation uncompacted, with both sides", async () => {
    await choose();
    await userEvent.click(await screen.findByText("Deploying"));
    // Scoped to the transcript, because the working-memory panel beside it shows
    // some of the same text on purpose — that overlap IS the comparison.
    const transcript = await screen.findByRole("region", { name: "Transcript" });
    await waitFor(() => expect(within(transcript).getByText("how do I deploy?")).toBeTruthy());
    expect(within(transcript).getByText("roll it out")).toBeTruthy();
  });

  /**
   * The live context, next to the record.
   *
   * The assertion that matters is the DIFFERENCE: the transcript holds both turns
   * and working memory holds one, which is what compaction did. An operator
   * asking "why doesn't it remember what I told it" is asking to see exactly this,
   * and before the panel existed there was no way to answer them.
   */
  it("shows what the agent still carries beside what it said", async () => {
    await choose();
    await userEvent.click(await screen.findByText("Deploying"));

    const live = await screen.findByRole("region", { name: "Working memory" });
    await waitFor(() => expect(within(live).getByText("how do I deploy?")).toBeTruthy());
    expect(within(live).queryByText("roll it out")).toBeNull();
    expect(within(live).getByText(/1,200 tokens/)).toBeTruthy();
  });

  // A conversation that ended cleanly keeps its transcript and nothing to resume
  // from. Said plainly rather than rendered as an empty box, which reads as a bug.
  it("says so plainly when a conversation carries no live context", async () => {
    model.readMemoryWorking.mockResolvedValue({
      found: false,
      version: 0,
      iteration: 0,
      tokens: 0,
      updatedAt: "",
      bytes: 0,
      readable: false,
    });
    await choose();
    await userEvent.click(await screen.findByText("Deploying"));

    const live = await screen.findByRole("region", { name: "Working memory" });
    await waitFor(() => expect(within(live).getByText(/no live context/)).toBeTruthy());
  });

  it("shows what the agent remembers about the person in the conversation", async () => {
    await choose();
    await userEvent.click(await screen.findByText("Deploying"));
    await screen.findByText("Prefers Go examples.");
    expect(screen.getByText(/Remembered about u-1/)).toBeTruthy();
  });

  // The deliberate omission. An operator rewriting what an agent believes about
  // somebody, with no audit trail, should be asked for explicitly.
  it("offers no way to edit a remembered fact", async () => {
    await choose();
    await userEvent.click(await screen.findByText("Deploying"));
    await screen.findByText("Prefers Go examples.");
    expect(screen.queryByRole("button", { name: /edit/i })).toBeNull();
    expect(screen.getByRole("button", { name: /forget prefers-go/i })).toBeTruthy();
  });

  it("asks before erasing a conversation", async () => {
    await choose();
    await userEvent.click(
      await screen.findByRole("button", { name: /erase the conversation deploying/i }),
    );
    await waitFor(() => expect(confirm).toHaveBeenCalled());
    expect(model.deleteMemoryThread).toHaveBeenCalledWith("int-1", "dr-octo", "t-1");
  });

  it("does not erase when the confirmation is declined", async () => {
    confirm.mockResolvedValue(false);
    await choose();
    await userEvent.click(
      await screen.findByRole("button", { name: /erase the conversation deploying/i }),
    );
    await waitFor(() => expect(confirm).toHaveBeenCalled());
    expect(model.deleteMemoryThread).not.toHaveBeenCalled();
  });

  it("searches conversations and remembered facts together", async () => {
    model.searchAgentMemory.mockResolvedValue([
      { kind: "turn", threadKey: "t-1", text: "how do I deploy?", seq: 1, score: 0.9 },
      { kind: "user", name: "prefers-go", text: "Prefers Go examples.", score: 0.7 },
    ]);
    await choose();
    await userEvent.type(await screen.findByLabelText("Search agent memory"), "deploy");
    await userEvent.click(screen.getByRole("button", { name: /^search$/i }));

    await screen.findByText("conversation");
    expect(screen.getByText(/remembered · prefers-go/)).toBeTruthy();
  });

  it("says so when an agent has recorded nothing", async () => {
    model.listMemoryThreads.mockResolvedValue({ threads: [] });
    await choose();
    await screen.findByText(/has not recorded a conversation yet/);
  });
});

// A late response must not land under a selection it does not belong to. Without
// the guard the viewer shows one agent's conversation under another agent's name,
// which is the worst kind of wrong for a tool whose job is telling you what a
// particular agent knows.
describe("AgentMemoryManager, when the selection changes mid-request", () => {
  it("discards a conversation that arrives after the agent was switched", async () => {
    model.listMemoryAgents.mockResolvedValue([
      { agentId: "dr-octo", threadCount: 1, lastActivityAt: "2026-08-02T00:00:00Z" },
      { agentId: "other", threadCount: 1, lastActivityAt: "2026-08-02T00:00:00Z" },
    ]);
    // Held open so the switch happens while the read is in flight.
    let release: (value: unknown) => void = () => {};
    model.readMemoryThread.mockImplementation(
      () =>
        new Promise((resolve) => {
          release = resolve;
        }),
    );

    await choose();
    await userEvent.click(await screen.findByText("Deploying"));
    await userEvent.selectOptions(screen.getByLabelText(/^Agent/), "other");

    release({
      thread: { threadKey: "t-1", title: "Deploying", userId: "u-1" },
      turns: [{ seq: 1, role: "user", text: "belongs to dr-octo", createdAt: "2026-08-01T00:00:00Z" }],
    });

    await waitFor(() => expect(screen.getByLabelText(/^Agent/)).toHaveValue("other"));
    expect(screen.queryByText("belongs to dr-octo")).toBeNull();
  });
});
