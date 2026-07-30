import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  EditorActionType,
  EditorStateProvider,
  useEditorState,
} from "../state/editorState";
import { emptyFlow, newBlock, type EditorDocument } from "../model/document";
import { EditorMetaProvider } from "../providers/EditorMetaProvider";
import { ConsoleProvider } from "./console";
import { FlowRunProvider } from "./FlowRunContext";
import { useBlockDebug } from "./useBlockDebug";
import type { RunTransport } from "./transport";

const transport: RunTransport = {
  status: async () => ({
    available: true,
    running: false,
    version: null,
    testAvailable: false,
    testVersion: null,
    testPath: null,
  }),
  start: async () => ({
    available: true,
    running: false,
    version: null,
    testAvailable: false,
    testVersion: null,
    testPath: null,
  }),
  stop: async () => {},
  sync: async () => {},
  evalCel: async () => ({ ok: true }),
  subscribeLogs: () => () => {},
  invoke: async () => ({
    ok: true,
    dropped: false,
    timedOut: false,
    output: "{}",
    logs: [],
  }),
};

/** Shows the debug state of the FIRST block in the flow, and lets a test place a mock. */
function Harness({ doc, index = 0 }: { doc: EditorDocument; index?: number }) {
  const { state, dispatch } = useEditorState();
  const flow = state.document.flows[0];
  const block = flow?.process[index];
  const debug = useBlockDebug(block?.id ?? "none");

  return (
    <div>
      <button
        onClick={() =>
          dispatch({
            type: EditorActionType.LOAD_INTEGRATION,
            data: {
              id: "orders.yaml",
              name: "orders",
              document: doc,
              folderId: null,
            },
          })
        }
      >
        load
      </button>
      <button onClick={() => debug?.toggleSpy()}>toggle spy</button>
      <button
        onClick={() =>
          debug?.setMock({ enabled: true, cases: [], default: { drop: true } })
        }
      >
        mock it
      </button>
      <p data-testid="address">{debug?.address ?? "-"}</p>
      <p data-testid="supported">{String(debug?.supported ?? false)}</p>
      <p data-testid="spied">{String(debug?.spied ?? false)}</p>
      <p data-testid="mocked">{String(debug?.mock?.enabled ?? false)}</p>
      <p data-testid="names">
        {state.document.flows[0]?.process.map((b) => b.name ?? "").join(",")}
      </p>
    </div>
  );
}

async function setup(doc: EditorDocument, index = 0) {
  const user = userEvent.setup();
  render(
    <EditorStateProvider>
      <EditorMetaProvider store={null}>
        <ConsoleProvider>
          <FlowRunProvider transport={transport}>
            <Harness doc={doc} index={index} />
          </FlowRunProvider>
        </ConsoleProvider>
      </EditorMetaProvider>
    </EditorStateProvider>,
  );
  await user.click(screen.getByText("load"));
  return user;
}

const at = (id: string) => screen.getByTestId(id).textContent;

/** A flow whose blocks are whatever the test says. */
function docOf(...blocks: ReturnType<typeof newBlock>[]): EditorDocument {
  const flow = emptyFlow("orders");
  flow.process = blocks;
  return { flows: [flow], connectors: [], processors: [], env: [] };
}

function named(type: string, name?: string) {
  const b = newBlock(type);
  if (name) b.name = name;
  return b;
}

describe("useBlockDebug", () => {
  it("gives a well-named block its address, and touches nothing", async () => {
    await setup(docOf(named("log", "audit")));

    expect(at("address")).toBe("orders.audit");
    expect(at("supported")).toBe("true");
    expect(at("names")).toBe("audit");
  });

  /**
   * The heart of the design. Two unnamed `log` blocks both answer to "log", so neither has
   * an address that could be written to a file and found again. Placing a spy on one NAMES
   * it — a real, visible edit to the document — so the key means something tomorrow.
   */
  it("names an ambiguous block when a spy is placed on it", async () => {
    const first = newBlock("log");
    const second = newBlock("log");
    const user = await setup(docOf(first, second), 1);

    // Ambiguous: no address yet, but naming can fix it, so the affordance is offered.
    expect(at("address")).toBe("-");
    expect(at("supported")).toBe("true");

    await user.click(screen.getByText("toggle spy"));

    expect(at("names")).toBe(",log-2");
    expect(at("address")).toBe("orders.log-2");
    expect(at("spied")).toBe("true");
  });

  it("names an ambiguous block when a mock is placed on it, and saves against that address", async () => {
    const user = await setup(docOf(newBlock("log"), newBlock("log")), 1);

    await user.click(screen.getByText("mock it"));

    expect(at("names")).toBe(",log-2");
    expect(at("address")).toBe("orders.log-2");
    expect(at("mocked")).toBe("true");
  });

  it("turns a spy back off", async () => {
    const user = await setup(docOf(named("log", "audit")));

    await user.click(screen.getByText("toggle spy"));
    expect(at("spied")).toBe("true");

    await user.click(screen.getByText("toggle spy"));
    expect(at("spied")).toBe("false");
  });

  /**
   * A flow whose name breaks the address grammar cannot be addressed at all, and no
   * renaming of a block would help — the flow's name is not ours to rewrite, since a
   * flow-ref addresses it by name. So the affordances hide rather than offer a mock that
   * could never be applied.
   */
  it("reports unsupported when no name could make the block addressable", async () => {
    const flow = emptyFlow("orders.eu");
    flow.process = [named("log", "audit")];
    await setup({ flows: [flow], connectors: [], processors: [], env: [] });

    expect(at("supported")).toBe("false");
    expect(at("address")).toBe("-");
  });
});
