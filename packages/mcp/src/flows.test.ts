import { describe, expect, it } from "vitest";
import { addFlow, deleteFlow, flowNames, getFlow, updateFlow } from "./flows";

/**
 * A definition with things worth NOT destroying: comments at the top, on a connector,
 * and inside a flow we are not touching.
 */
const DEF = `# The orders service.
service:
  name: orders

connectors:
  # Talks to the payment provider.
  - name: stripe
    type: rest

flows:
  - name: charge
    process:
      - type: rest
        name: call-stripe # the real one
        settings:
          connector: stripe

  - name: audit
    process:
      - type: log
        name: write-it
`;

describe("flow splicing", () => {
  it("lists the flow names in file order", () => {
    expect(flowNames(DEF)).toEqual(["charge", "audit"]);
  });

  describe("getFlow", () => {
    it("returns one flow's YAML, with the comments it carried", () => {
      const out = getFlow(DEF, "charge");
      expect(out).toContain("name: charge");
      expect(out).toContain("# the real one");
      // The flow itself, not a one-element list.
      expect(out.trimStart().startsWith("-")).toBe(false);
      // And only that flow.
      expect(out).not.toContain("audit");
    });

    it("names the flows that do exist when one doesn't", () => {
      expect(() => getFlow(DEF, "refund")).toThrow(
        'no flow named "refund" in this integration (it has: "charge", "audit")',
      );
    });
  });

  describe("updateFlow", () => {
    // The whole reason this is an AST splice and not a parse/serialize round-trip.
    it("leaves every other part of the file — and its comments — exactly as it was", () => {
      const out = updateFlow(
        DEF,
        "charge",
        "name: charge\nprocess:\n  - type: log\n    name: pretend\n",
      );

      expect(out).toContain("# The orders service.");
      expect(out).toContain("# Talks to the payment provider.");
      expect(out).toContain("name: stripe");
      // The untouched flow survives whole, comment included.
      expect(out).toContain("name: write-it");
      // The replaced flow is gone, and the new one is in its place.
      expect(out).not.toContain("call-stripe");
      expect(out).not.toContain("# the real one");
      expect(out).toContain("name: pretend");
      expect(flowNames(out)).toEqual(["charge", "audit"]);
    });

    it("keeps comments written into the replacement", () => {
      const out = updateFlow(DEF, "charge", "name: charge\n# stubbed for now\nprocess: []\n");
      expect(out).toContain("# stubbed for now");
    });

    // A rename is a normal edit; forcing it through delete+add would move the flow to the
    // end of the file for no reason.
    it("renames the flow in place when the replacement carries a new name", () => {
      const out = updateFlow(DEF, "charge", "name: charge-card\nprocess: []\n");
      expect(flowNames(out)).toEqual(["charge-card", "audit"]);
    });

    it("refuses a rename onto a name another flow already holds", () => {
      expect(() => updateFlow(DEF, "charge", "name: audit\nprocess: []\n")).toThrow(
        /another flow already has that name/,
      );
    });

    it("errors when no flow answers to the name", () => {
      expect(() => updateFlow(DEF, "refund", "name: refund\nprocess: []\n")).toThrow(
        /no flow named "refund"/,
      );
    });

    // The commonest way to hold this tool wrong: passing the whole file, or the flows list.
    it("rejects a definition that is not a single flow mapping", () => {
      expect(() => updateFlow(DEF, "charge", "- name: charge\n  process: []\n")).toThrow(
        /must be a YAML mapping/,
      );
    });

    it("rejects a flow with no name", () => {
      expect(() => updateFlow(DEF, "charge", "process: []\n")).toThrow(/needs a `name`/);
    });
  });

  describe("addFlow", () => {
    it("appends the flow and leaves the rest alone", () => {
      const out = addFlow(DEF, "name: refund\nprocess: []\n");
      expect(flowNames(out)).toEqual(["charge", "audit", "refund"]);
      expect(out).toContain("# Talks to the payment provider.");
    });

    // Two flows of one name is a config the runtime cannot address unambiguously.
    it("refuses a name that is already taken", () => {
      expect(() => addFlow(DEF, "name: audit\nprocess: []\n")).toThrow(/already exists/);
    });

    // A config declaring only connectors is a legitimate starting point.
    it("creates the flows list when the definition has none", () => {
      const out = addFlow("service:\n  name: orders\n", "name: charge\nprocess: []\n");
      expect(flowNames(out)).toEqual(["charge"]);
    });
  });

  describe("deleteFlow", () => {
    it("removes just that flow", () => {
      const out = deleteFlow(DEF, "charge");
      expect(flowNames(out)).toEqual(["audit"]);
      expect(out).toContain("# The orders service.");
      expect(out).toContain("name: write-it");
    });

    it("errors when no flow answers to the name", () => {
      expect(() => deleteFlow(DEF, "refund")).toThrow(/no flow named "refund"/);
    });

    /**
     * A doc comment above a flow describes *that* flow. Leaving it behind does not merely
     * litter: the next flow down slides up under it and inherits the caption, so the file
     * ends up actively lying about a flow nobody touched.
     */
    describe("the comment block above a flow", () => {
      const DOCUMENTED = `service:
  name: orders

flows:
  # Charges the card.
  # Do not run this one for fun.
  - name: charge
    process:
      - type: log
        name: a

  # Writes the audit trail.
  - name: audit
    process:
      - type: log
        name: b
`;

      it("goes with the flow it documented", () => {
        const out = deleteFlow(DOCUMENTED, "charge");
        expect(out).not.toContain("Charges the card");
        expect(out).not.toContain("Do not run this one for fun");
      });

      it("does not re-caption the flow below it", () => {
        const out = deleteFlow(DOCUMENTED, "charge");
        expect(out).toContain("# Writes the audit trail.\n  - name: audit");
        expect(flowNames(out)).toEqual(["audit"]);
      });

      it("survives an update of the flow it belongs to", () => {
        const out = updateFlow(
          DOCUMENTED,
          "charge",
          "name: charge\nprocess:\n  - type: log\n    name: c\n",
        );
        expect(out).toContain("# Charges the card.");
        expect(out).toContain("name: c");
      });

      // The other half of the rule. A note set apart by a blank line is talking about the
      // file or the list, not about the flow that happens to come next, so deleting that
      // flow must leave it alone.
      it("leaves a list-level note, which a blank line sets apart, where it is", () => {
        const CAPTIONED = `flows:
  # Every flow here is triggered by the API.

  - name: charge
    process:
      - type: log
        name: a

  - name: audit
    process:
      - type: log
        name: b
`;
        const out = deleteFlow(CAPTIONED, "charge");
        expect(out).toContain("# Every flow here is triggered by the API.");
        expect(flowNames(out)).toEqual(["audit"]);
      });
    });
  });

  it("reports a definition that is not valid YAML as such", () => {
    expect(() => flowNames("flows: [\n")).toThrow(/not valid YAML/);
  });

  /**
   * The reason this is a textual splice and not an AST rewrite. Re-serializing the parsed
   * document re-decides how every scalar is written — long quoted CEL expressions get
   * folded at the line width, folded block scalars (`prompt: >`) get flattened — and it
   * does that to nodes the caller never touched. None of it shows up on the short flows
   * above; all of it shows up on real integration files.
   */
  describe("formatting of the flows it was not asked to touch", () => {
    const AWKWARD = `service:
  name: orders

flows:
  - name: keeper
    process:
      - type: ai-mapping
        name: summarize
        settings:
          prompt: >
            Summarize the text found in body.text in a friendly style,
            in exactly one sentence.
          value: '{"who": vars.customerName, "what": body.amount, "when": vars.at, "where": vars.region, "why": "because"}'

  - name: target
    process: []
`;

    it("does not flatten a folded block scalar in another flow", () => {
      const out = updateFlow(AWKWARD, "target", "name: target\nprocess:\n  - type: log\n    name: x\n");
      expect(out).toContain("prompt: >");
      expect(out).toContain("in a friendly style,\n");
    });

    it("does not re-wrap a long line in another flow", () => {
      const out = updateFlow(AWKWARD, "target", "name: target\nprocess: []\n");
      expect(out).toContain(
        `value: '{"who": vars.customerName, "what": body.amount, "when": vars.at, "where": vars.region, "why": "because"}'`,
      );
    });

    // The strongest statement of the guarantee, and the one that caught every bug here:
    // taking a flow out and putting the very same flow back leaves the file identical.
    it("get→update of an unchanged flow is a byte-for-byte no-op", () => {
      for (const name of flowNames(AWKWARD)) {
        expect(updateFlow(AWKWARD, name, getFlow(AWKWARD, name)), name).toBe(AWKWARD);
      }
      for (const name of flowNames(DEF)) {
        expect(updateFlow(DEF, name, getFlow(DEF, name)), name).toBe(DEF);
      }
    });

    // A flow the caller wrote with a block scalar keeps it: the text goes in verbatim.
    it("keeps a block scalar the caller wrote in the replacement", () => {
      const out = updateFlow(
        DEF,
        "charge",
        "name: charge\nprocess:\n  - type: log\n    name: shout\n    settings:\n      message: |\n        line one\n        line two\n",
      );
      expect(out).toContain("message: |");
      expect(out).toContain("line one\n");
    });
  });

  // Flows sit a blank line apart. Adding or removing one has to keep it that way, or the
  // gaps double up and the file drifts away from how it was written.
  describe("blank lines between flows", () => {
    it("add then delete leaves the file exactly as it was", () => {
      const added = addFlow(DEF, "name: refund\nprocess: []\n");
      expect(added).toContain("\n  - name: refund");
      expect(deleteFlow(added, "refund")).toBe(DEF);
    });

    it("deleting a middle flow does not leave a double gap", () => {
      expect(deleteFlow(DEF, "charge")).not.toMatch(/\n\n\n/);
    });

    it("deleting the last flow does not leave the file ending on blank lines", () => {
      expect(deleteFlow(DEF, "audit")).not.toMatch(/\n\n+$/);
    });
  });
});
