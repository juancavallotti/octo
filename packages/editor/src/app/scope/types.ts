/**
 * What the editor believes a CEL expression can see at one point in a flow.
 *
 * The runtime types every message variable as `dyn` — `body` genuinely is anything —
 * so this is not a type system and cannot become one by trying harder. It is a record
 * of what can be *justified* from the document: a variable a block upstream declares,
 * a key present in the test input the flow is run with. Anything else is `unknown`,
 * and saying so is the point. A suggestion that turns out not to exist at run time is
 * worse than an empty menu, because it teaches the user to trust the menu.
 */

/** Where a belief came from, weakest last. Rendered as the reason in the menu. */
export type Origin =
  /** The runtime always provides it (`body`, `env`, `now`). */
  | "declared"
  /** A block upstream is configured to set it. */
  | "inferred"
  /** It was present in a saved test input. */
  | "sample";

export type ValueShape =
  /** No belief at all. Offers nothing. */
  | { kind: "unknown" }
  /** It exists; its shape is not knowable. */
  | { kind: "dyn" }
  | { kind: "string" }
  | { kind: "number" }
  | { kind: "bool" }
  | { kind: "null" }
  | { kind: "timestamp" }
  | { kind: "list"; of: ValueShape }
  | {
      kind: "object";
      fields: Record<string, Field>;
      /**
       * Whether keys beyond `fields` are possible. Almost always true: knowing three
       * keys of a body is not knowing that there are only three.
       */
      open: boolean;
    };

export interface Field {
  shape: ValueShape;
  origin: Origin;
  /** Why we think so — "set by set-variable", "seen in test input". */
  note?: string;
  /**
   * False when only some paths through the flow set it: a variable set inside one
   * switch case is not there when another case ran.
   */
  certain: boolean;
}

/** The root variables in scope at one point, and what is known about each. */
export interface Scope {
  roots: Record<string, Field>;
}

/** What one block does to the scope of everything after it. */
export interface Contribution {
  /** Variables it sets. */
  setVars?: Record<string, Field>;
  /** Variables it removes. */
  deleteVars?: string[];
  /**
   * What becomes of the body: left alone, replaced by something we cannot describe
   * (which erases what we knew), or replaced by something we can.
   */
  body?: "keep" | "opaque" | { kind: "shape"; shape: ValueShape };
}

/** Which CEL expression is being edited, and therefore which scope applies. */
export type CelSite =
  /** A block's own settings: the message as that block RECEIVES it. */
  | { kind: "block"; blockId: string }
  /** A flow's result: the message after its last block ran. */
  | { kind: "flow-output"; flowId: string }
  /** A source's payload expression, which has its own, much smaller, scope. */
  | { kind: "source"; flowId: string }
  /** Anything not tied to a position — a template, the CEL tester. */
  | { kind: "document" };
