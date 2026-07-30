import { suiteFileName } from "../../suite/naming";

/**
 * The suite a flow starts with.
 *
 * Written as text rather than built through the model and serialized, because the
 * comments are most of the value: this file is the first thing a user sees of the
 * format, and it is committed, so it teaches whoever opens it next. Serializing would
 * drop every line of it.
 *
 * The one case it ships with asserts only that the flow completed and did not fail —
 * which an empty `expect` really does assert — so a brand new suite passes, and the user
 * sees a green run before they have written a single expectation.
 */
export function scaffoldSuite(flow: string): string {
  return `# Tests for the "${flow}" flow, run by dolphin.
#
#   dolphin test ${suiteFileName(flow)}
#
# Each case invokes the flow once with the input you give it, stands in for the blocks
# you mock, and checks what came back.
flow: ${flow}

# Environment every case runs with. A config's env resolves when it LOADS, before any
# block runs, so a flow whose connector reads \${SOME_KEY} needs one here even if you
# mock the block that uses it. These are fake by construction, which is what makes them
# safe to commit.
# env:
#   SOME_KEY: not-a-real-key

# Stand-ins for blocks, in every case unless a case overrides the address.
# mocks:
#   ${flow}.some-block:
#     default:
#       body: {ok: true}

cases:
  - name: it runs
    # input:
    #   data: {}      # the message body the flow is called with
    #   vars: {}      # what a source would normally have set
    expect: {}
      # body: {}      # exact: the whole body, deep-equal
      # vars: {}      # subset: only the variables named here are checked
      # that: []      # CEL over the message, each expression must be true
      #
      # ...or one of the other two things a flow can do:
      # dropped: true
      # error: "some text the failure contains"
`;
}
