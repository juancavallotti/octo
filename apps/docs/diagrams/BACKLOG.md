# Diagram backlog

Two kinds of entry. **Replacing** means the page already draws this in ASCII, so
the shape is settled and the drawing is a straight upgrade. **New** means the page
explains something structural in prose alone and a reader has to build the picture
in their head.

The workflow, non-destructively: a scene is generated here, opened in Excalidraw,
improved, and saved back as `<name>.excalidraw` with **Export → Save to...**. No
share links, nothing overwritten. `node export.mjs <name>` renders the PNG.

## Done

| Name | Page | State |
| --- | --- | --- |
| `self-healing-loop` | platform/self-healing-loop | Drawn, exported, in the page |
| `platform-architecture` | platform/architecture | Generated and exported, **not yet in the page**: needs a pass by hand, and the ASCII block comes out when it lands |
| `agent-memory-tiers` | ai/agent-memory | Drawn, exported, in the page |
| `kv-tiers` | platform/kv-and-storage | Drawn, exported, in the page |

## Replacing ASCII

| Name | Page | What it has to show |
| --- | --- | --- |
| `editor-how-it-works` | editor/how-it-works | The browser running `@octo/editor`, the BFF behind it, and the runtime it drives. Two processes and one browser, which the current box-drawing sandwich makes look like one thing |
| `processing-pipeline` | runtime/processing-pipeline | connector, source, the bounded channel, the per-flow worker pool, the root chain. The channel is where backpressure comes from and that is invisible in prose |
| `two-pools` | runtime/processing-pipeline | The second level: per-flow workers each running a whole chain, and the one shared pool the composites schedule onto. Currently a second ASCII block that reads as a variation of the first |
| `queues-vs-topics` | runtime/clustering | One subject delivering to exactly one replica, beside one subject delivering to every replica. The single most confusable pair in the runtime |
| `gcp-topology` | deploy/gcp-terraform | DNS records to a static IP, one VM running k3s, Traefik and cert-manager in front of the pods. People read this while typing, so it earns a picture |
| `slack-triage-loop` | guides/agentic-self-healing | alert, triage, Slack thread and email, the person's reply, refine or authorize, final email. Already a loop drawn in dashes |
| `log-shipping` | runtime/monitoring | Pod to `internal.logs` to the observability service to the viewer, with block events as the separate second stream they are |

## New

| Name | Page | What it has to show |
| --- | --- | --- |
| `message-lifecycle` | concepts/state-and-data | One message from the source that built it to the terminal event: what the body is, what `vars` carry alongside it, and which parts survive each block. The model everything else assumes |
| `error-paths` | concepts/error-handling | The three different recoveries side by side: the flow-level `error:` pipeline, the scoped `handle-errors`, and `validate`'s `onReject` stopping the chain. They are three shapes, not three settings |
| `agent-turn-loop` | ai/agents | The model turn, tools dispatched as sub-flows, the `events` sub-flow watching from the side, and the answer. Readers who have not built an agent do not picture the loop |
| `tool-authorization` | ai/tool-authorization | A run parking mid-flight, the `authorizationId` going out, a person answering, the run resuming from where it stopped, and the timeout path. Time is the axis, which prose handles badly |
| `agent-memory-tiers` | ai/agent-memory | The thread's conversation, the uncompacted history behind it, and durable user memory keyed by `agentId`. Three tiers people currently have to hold in their head |
| `deployment-lifecycle` | platform/deployments | Integration to snapshot to deployment to Deployment and pods, and the arrow back that a rollback is |
| `dev-run` | platform/dev-runs | One pod holding two containers: the dev sidecar owning the workspace, the runtime watching it. The sidecar-pulls-and-runtime-watches split is the whole feature |
| `dolphin-run` | testing/index | One case as one `octo` process, with mocks and spies baked into the flow tree before it starts. Explains at a glance why a mock replaces a block rather than intercepting a call |
| `services-providers` | extending/runtime-services | One interface, three providers behind it: standalone, k8s, api. Currently a table doing a picture's job |
| `platform-api-contract` | extending/platform-api | The runtime asking discovery what you implement, then talking only to what you declared, with your own infrastructure behind your server |

## Probably not worth drawing

Reference pages (connectors, blocks, CEL) are settings tables; a picture would sit
above the thing people came for. Same for the Helm chart. File trees and the
decision tree in `extending/index` are already the right shape as text.
