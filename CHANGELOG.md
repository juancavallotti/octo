# Changelog

## [0.8.2](https://github.com/juancavallotti/octo/compare/v0.8.1...v0.8.2) (2026-08-15)


### Features

* **cel:** add hmac, hex, and constant-time compare functions ([6818839](https://github.com/juancavallotti/octo/commit/6818839de164f802730723b8c06b9a7c4d72d124))
* **engine:** add a cli-run block that runs a local program ([0298e5b](https://github.com/juancavallotti/octo/commit/0298e5bb4a0ab326eebae89e230f2aa971bc1c9b))
* **engine:** resolve cli-run programs through $PATH and make allow optional ([b5ab18c](https://github.com/juancavallotti/octo/commit/b5ab18ce08765fb88d558f7c84885f0befae09bb))
* MCP protocol gaps, webhook-signature CEL functions, and a cli-run block ([5a9c838](https://github.com/juancavallotti/octo/commit/5a9c83848f30cdb623d9709b08dc9d326fa459ed))
* **mcp:** advertise tool titles, annotations, and output schemas ([8bddc43](https://github.com/juancavallotti/octo/commit/8bddc43e52eb254da192e4d7ceeb7aa32c7efd9e))
* **mcp:** negotiate the protocol version and advertise honest capabilities ([bc51ec6](https://github.com/juancavallotti/octo/commit/bc51ec6716e9bd66ada4819c6242ada3a02f4adf))
* **mcp:** serve resources/templates/list and templated resource reads ([17c25ca](https://github.com/juancavallotti/octo/commit/17c25ca6074955a6756e3dbda18242dfa2a309c6))
* **platform:** add a Running apps bucket to the folder tree ([e7daf9e](https://github.com/juancavallotti/octo/commit/e7daf9e56c365cae153853662a42b4383624fd7c))
* **platform:** add rollout and pod logs to the deployments page ([b4c5503](https://github.com/juancavallotti/octo/commit/b4c5503beda72a4fa50eed6410294da077077c34))
* **platform:** put icons on the queue stat cards ([7771e2f](https://github.com/juancavallotti/octo/commit/7771e2f9d79b06ef4b7a60cc034ccae923d17123))


### Bug Fixes

* **editor:** register the Terminal icon so cli-run is not a generic box ([6489546](https://github.com/juancavallotti/octo/commit/648954610e7409890713ccdc3de5c8082c780217))
* **engine:** a cli-run events path observes the output, it does not eat it ([94b9991](https://github.com/juancavallotti/octo/commit/94b99918e19ab0bb447dc3530818e3b9b6e063e4))
* **engine:** a hung-up caller actually stops the command, and 14 review fixes ([761ebd5](https://github.com/juancavallotti/octo/commit/761ebd52759ff0d62cd63564a266984ec6be6532))
* **engine:** fail at build for a program the allow list can never permit ([af0d02a](https://github.com/juancavallotti/octo/commit/af0d02a4efc8460916577a85a6e8898fcfe6dceb))
* **orchestrator:** tag the agent install with the platform version ([74af7d2](https://github.com/juancavallotti/octo/commit/74af7d24befa48ad63d317a59a7a442e80c7bfee))
* **platform:** a batch of PaaS UX fixes across deployments, folders and queues ([5d45515](https://github.com/juancavallotti/octo/commit/5d4551591af6c35db1cd58423379e1605d92b5ce))
* **platform:** give a deployment card's tag and tracing pills their own row ([05fe0c3](https://github.com/juancavallotti/octo/commit/05fe0c3461b196020a010b3669ad92866c90c8d4))
* **platform:** give each queue counter one place on the page ([cec2ea7](https://github.com/juancavallotti/octo/commit/cec2ea75df727b3923759f4947aaa322ca65e1e4))
* **platform:** ignore rollout tags that arrive after the dialog moved on ([ae880cb](https://github.com/juancavallotti/octo/commit/ae880cb23578a5aa76c9501b51d9ff8021a1f0fc))
* **platform:** stop attributing connection-wide NATS counters to a subject ([20c8d97](https://github.com/juancavallotti/octo/commit/20c8d97f485fce37633685ea867c1a9d688643b6))
* **terraform:** the release plans with a digest-pinned values file ([066b7b8](https://github.com/juancavallotti/octo/commit/066b7b8a97da13b32440aebb503b58bf967d8b13))
* **terraform:** the release plans with a digest-pinned values file ([ced8d1c](https://github.com/juancavallotti/octo/commit/ced8d1c46d1eea1f928ddef6b942dadc1a8262b2))


### Refactoring

* **engine:** make the events emitter reusable beyond ai-agent ([3f706f9](https://github.com/juancavallotti/octo/commit/3f706f91ebe398953ce7f820c1726cee62fb316d))


### Documentation

* **editor:** say where the server-safe helpers live ([8f99558](https://github.com/juancavallotti/octo/commit/8f995583b781ac1d6286aa6da9fd14623cdc5a91))
* **editor:** say where the server-safe helpers live ([34bc67a](https://github.com/juancavallotti/octo/commit/34bc67a366b4c0de558b68ab9864a57d4d9d387b))
* **guides:** add guides for verifying webhooks and running commands ([931853c](https://github.com/juancavallotti/octo/commit/931853c50bfcf46b0c75411d756a2e66670773f7))

## [0.8.1](https://github.com/juancavallotti/octo/compare/v0.8.0...v0.8.1) (2026-08-12)


### Features

* **agent:** dr. octo remembers facts about the person he is talking to ([ccb7eb7](https://github.com/juancavallotti/octo/commit/ccb7eb76ee028404c96f61717bdfb6c6f032edde))
* **agent:** dr. octo speaks markdown and can move the user around ([91d7d39](https://github.com/juancavallotti/octo/commit/91d7d39842433e77f300e94b9edb970ef721f34d))
* **agent:** dr. octo, the platform agent, as an octo app ([01dbefd](https://github.com/juancavallotti/octo/commit/01dbefdf5ebfeffb65565edec806710453f3ff0c))
* **agent:** let dr. octo read stored logs and traces ([6092629](https://github.com/juancavallotti/octo/commit/60926294f33e0d2f7542c7cbe1f32ed0163e046e))
* **helm:** a nightly job that enforces the retention policy ([ce3a2cd](https://github.com/juancavallotti/octo/commit/ce3a2cd448a4f83c9ac6cf787e9282e125be1112))
* **logs:** a store for the site's retention policy ([015fed1](https://github.com/juancavallotti/octo/commit/015fed11905ebc88be52cfa9837b66e92ebfc157))
* **logs:** prune logs and traces past a cutoff ([321eca8](https://github.com/juancavallotti/octo/commit/321eca8213ec8e827b52b1c7667fe420251cdca1))
* **logs:** publish an openapi description of the api ([3acf976](https://github.com/juancavallotti/octo/commit/3acf976adcd9c2ad541bb8730db7e261a7d47965))
* **logs:** serve the retention policy and the sweep behind it ([c398cae](https://github.com/juancavallotti/octo/commit/c398cae429e964444c462be0be450fff90fbdf58))
* **orchestrator:** a store for site-wide settings ([f5ff528](https://github.com/juancavallotti/octo/commit/f5ff52849ec6262ca7c38627d51b49a1303fff56))
* **orchestrator:** describe every route the api serves ([8ddcde8](https://github.com/juancavallotti/octo/commit/8ddcde8f9150922221f74d3f9bb2dcd1c0374ae8))
* **orchestrator:** freeze the agent's edits before replacing them ([bcaee1d](https://github.com/juancavallotti/octo/commit/bcaee1d95a5a475bcf80876778e2880d5d4d3f9b))
* **orchestrator:** install dr. octo as an integration ([8b76263](https://github.com/juancavallotti/octo/commit/8b76263a3a09074d396893547c19f64fd638ad7f))
* **orchestrator:** make a settings save atomic ([e898f76](https://github.com/juancavallotti/octo/commit/e898f76235344019adc5fd451ab511f09c4bc7cb))
* **orchestrator:** per-deployment platform access grants ([049bf5c](https://github.com/juancavallotti/octo/commit/049bf5c92d7a973507b5204acbd4a45ed1062027))
* **orchestrator:** publish an openapi description of the api ([b9157b2](https://github.com/juancavallotti/octo/commit/b9157b2b9d9b4831bd1b5f55ce7d48a6588e4a1f))
* **orchestrator:** send email through the site's provider ([21ebbee](https://github.com/juancavallotti/octo/commit/21ebbee92992f23b889a7f6326122985ea9add28))
* **orchestrator:** send mail through resend ([478eaab](https://github.com/juancavallotti/octo/commit/478eaab778c96e55ee0de43e0ebff545651c3839))
* **orchestrator:** serve the site settings routes ([5b113af](https://github.com/juancavallotti/octo/commit/5b113af35502b98e4274fee13463000186242901))
* **orchestrator:** the site's email settings ([1c55fa7](https://github.com/juancavallotti/octo/commit/1c55fa7278f5eb8c201e6d3ac443a411e34ad782))
* **orchestrator:** the site's llm provider settings ([9e1bd8a](https://github.com/juancavallotti/octo/commit/9e1bd8a7fbde99f2449c1c3fd42fb63bfa099be8))
* **orchestrator:** trace the agent from his first deploy ([c68a368](https://github.com/juancavallotti/octo/commit/c68a368d4a6a05398b08fc968feb0335dfaddc42))
* **platform:** a chat panel for dr. octo on every signed-in page ([2da40b7](https://github.com/juancavallotti/octo/commit/2da40b7cacec5ad10be2ef286017a2d3312e249c))
* **platform:** a client for the site's retention policy ([75a1c5e](https://github.com/juancavallotti/octo/commit/75a1c5e9f683ee5c1a32c491147886fde98d4fac))
* **platform:** a way back to the agent this orchestrator ships ([37bc48d](https://github.com/juancavallotti/octo/commit/37bc48d6183476f590b64e730f4e30c6caf9fa02))
* **platform:** an admin page for the platform agent ([98df6a0](https://github.com/juancavallotti/octo/commit/98df6a09d57860f34f306ad3728b93ec0af888cf))
* **platform:** an admin section for site-wide settings ([6c3fd86](https://github.com/juancavallotti/octo/commit/6c3fd86a991f60e5a89d914c3ee12515ac0934f8))
* **platform:** ask for platform access in the deploy dialog ([8b4284a](https://github.com/juancavallotti/octo/commit/8b4284a12b7c81468663f7d34faa4fc798fc638a))
* **platform:** give the agent panel the same shape as an integration ([1b9a822](https://github.com/juancavallotti/octo/commit/1b9a822f5bbaa62e1e6b229b11f7eb0b4c348bc0))
* **platform:** navigation between the admin pages ([a60d40a](https://github.com/juancavallotti/octo/commit/a60d40a8ec9eb2e9d8bd5c927a91c6d9a52413cf))
* **platform:** show the thinking, open up the tool calls, and collapse instead of closing ([0082e49](https://github.com/juancavallotti/octo/commit/0082e49bc3bdf560b0b8e7bb7326c7d6e0c9a9d3))
* **platform:** stream a conversation with the agent to the browser ([ccbaac2](https://github.com/juancavallotti/octo/commit/ccbaac20ae2a9c9e730bafa9d8662c01f8db1e27))
* **platform:** the data retention admin page ([d18a3e0](https://github.com/juancavallotti/octo/commit/d18a3e0ef6095737742365bad5cdd830c4554561))
* **platform:** the data retention admin page ([27d0be5](https://github.com/juancavallotti/octo/commit/27d0be5abc533cf57dd63873699fff2705506b22))
* **runtime:** a rest block that builds the whole request from expressions ([f199073](https://github.com/juancavallotti/octo/commit/f1990730c6214ef7260f88d7ee65a39143d5afbc))
* **runtime:** a system subject convention for the platform event plane ([439ab6c](https://github.com/juancavallotti/octo/commit/439ab6c3db2128ae689e993d086a191dc1226b2f))
* **runtime:** an ai-agent states its turn and the shape of its answer ([53468af](https://github.com/juancavallotti/octo/commit/53468af1835b37a6314c1eced338788921a5ee73))
* **runtime:** openai reasons through the responses api ([0d0009a](https://github.com/juancavallotti/octo/commit/0d0009a7f9f548ae3138b63a07e6b55a68d14cca))


### Bug Fixes

* **agent:** a 404 is an out-of-date service, not a missing permission ([6d9a47a](https://github.com/juancavallotti/octo/commit/6d9a47a43937b713c49a92c59b5d40c84ebf4866))
* **agent:** bound what dr. octo remembers, and say what the race costs ([ce1a87b](https://github.com/juancavallotti/octo/commit/ce1a87b0e32d5e1c93cc8c75db5024b4f730b79b))
* **agent:** dr. octo is asked a question, and answers in prose ([1b39e9b](https://github.com/juancavallotti/octo/commit/1b39e9bd6be5e807da2eb86242bdd0447e885c0f))
* **agent:** scope dr. octo's memory to the user, and keep email from the model ([04629b7](https://github.com/juancavallotti/octo/commit/04629b7047b772be76a65a65844e347a7f4ae79b))
* **agent:** stream the thinking, which is most of what the model produces ([ea69b5e](https://github.com/juancavallotti/octo/commit/ea69b5ed3d951b6b895db2b97b764b1acbc9d115))
* **logs:** a retention save must name both windows ([37897b2](https://github.com/juancavallotti/octo/commit/37897b23c481f8411e2307a21d0c704224840ecd))
* **orchestrator:** "gone" and "I could not tell" are different answers ([fe73062](https://github.com/juancavallotti/octo/commit/fe73062279e5cef5d0ab933de6d95fa4196dd7fe))
* **orchestrator:** a manually undeployed agent can be deployed again ([78895e5](https://github.com/juancavallotti/octo/commit/78895e50e534f2dc7a384ba2ad4ee4efe1650e51))
* **orchestrator:** a provider change invalidates the stored llm key ([3262c2a](https://github.com/juancavallotti/octo/commit/3262c2a4519b2d21add5ce3396cba1b7fd5d187d))
* **orchestrator:** describe responses as they are actually served ([96cfacc](https://github.com/juancavallotti/octo/commit/96cfaccb1dc5c1addcc86fb5bdb08bf1972e78ce))
* **orchestrator:** five gaps in what installing the agent leaves behind ([ef6ff87](https://github.com/juancavallotti/octo/commit/ef6ff874ddb2f2ec6d713b0798ede83c3693374c))
* **orchestrator:** LOGS_URL is the grant's to set, and nobody else's ([e06e7ec](https://github.com/juancavallotti/octo/commit/e06e7ec7de8f00ba4cec1fa8465bde21e6f44fe9))
* **orchestrator:** make a failed agent install retryable ([26fe96e](https://github.com/juancavallotti/octo/commit/26fe96e099db890c74996e3e1b7e8258fa729765))
* **orchestrator:** redeploy the agent after a manual undeploy ([92e85d7](https://github.com/juancavallotti/octo/commit/92e85d7dddbc209bf9f004d644dc849c1667dba8))
* **orchestrator:** save email settings under a row lock ([2d7ef58](https://github.com/juancavallotti/octo/commit/2d7ef5817320c93c88dce03974abe29afc9edd32))
* **orchestrator:** save llm settings under a row lock ([a916a08](https://github.com/juancavallotti/octo/commit/a916a0860ea34ccc98e8949aac215a4374a6437c))
* **orchestrator:** the agent's provider key must be a valid platform secret name ([f2ecedd](https://github.com/juancavallotti/octo/commit/f2ecedda258de0c781568ecf8dd524cea64866ed))
* **orchestrator:** trim addresses and subject before sending ([e5cf1c7](https://github.com/juancavallotti/octo/commit/e5cf1c796b303cfb19eb080cb0afe98e96127bf6))
* **platform:** a sweep's report does not outlive the sweep ([cbe3ce1](https://github.com/juancavallotti/octo/commit/cbe3ce1128445e9986f693cf17f31600b44bf602))
* **platform:** announce a sweep's outcome, and say 03:00 is a default ([9bea28b](https://github.com/juancavallotti/octo/commit/9bea28bdf204de5924816a980673cdb70dc4711f))
* **platform:** check an agent frame's fields, not just its type ([175cde5](https://github.com/juancavallotti/octo/commit/175cde55d8e08b5c03c583c05d5de7b40b74c625))
* **platform:** classify rest-dynamic in the trace waterfall ([fa24e47](https://github.com/juancavallotti/octo/commit/fa24e47d7d2fe99ec07f485d0706ceb6944823eb))
* **platform:** do not let an admin form save before it has loaded ([5411692](https://github.com/juancavallotti/octo/commit/541169212c07e40318e239c61a71fc0ffcc77e99))
* **platform:** normalise what the admin forms submit ([d55d2bc](https://github.com/juancavallotti/octo/commit/d55d2bc25417f74b7b6057546e85301c8b8d7b4b))
* **platform:** one header on the agent page, and a tab that leads to it ([b6724ca](https://github.com/juancavallotti/octo/commit/b6724ca81030884aee1c2ac25cbb83d6bb5bbb37))
* **platform:** one run at a time, guarded by the controller and not by state ([d6197b1](https://github.com/juancavallotti/octo/commit/d6197b1845ed39e8ac71b5653b1a10f0337d5466))
* **platform:** show the guardrail's reply, and let a stopped run bow out quietly ([497e83e](https://github.com/juancavallotti/octo/commit/497e83ee165d52b15b49335e01bd68c9d4457b5f))
* **platform:** the chat panel needs the write roles, not just a session ([08af560](https://github.com/juancavallotti/octo/commit/08af5605a8b45c46b81e2245d8732c79a143aaca))
* **platform:** two ways the chat panel broke outside a browser tab on https ([8b2d525](https://github.com/juancavallotti/octo/commit/8b2d52550b714defbad26fb65903f25837dd321d))
* **runtime:** a gemini tool call needs an id of its own ([69907ad](https://github.com/juancavallotti/octo/commit/69907adc1cfe72ffd3122a7ba794d3d90153298a))
* **runtime:** a system subject may be published to, not subscribed to ([ccbd9f2](https://github.com/juancavallotti/octo/commit/ccbd9f24c63530de2ed9768e5462a034f15bb62a))
* **runtime:** an echoed reasoning item must carry a summary, even an empty one ([98bb036](https://github.com/juancavallotti/octo/commit/98bb03669c3e0d63ca93507b3e899adbc832cec1))
* **runtime:** close three gaps in what rest-dynamic accepts ([5fdaa60](https://github.com/juancavallotti/octo/commit/5fdaa6059785d687eb64ba081836374e3de103d4))
* **runtime:** drop the "off" alias, and scope the tool-use warning to the model ([ca32f14](https://github.com/juancavallotti/octo/commit/ca32f14fe1e500b899009b0bdd3874251c41b47a))
* **runtime:** let openai and gemini agents actually call their tools ([6408d20](https://github.com/juancavallotti/octo/commit/6408d2055cdaa7795766900ef81d3aba58d19fe9))
* **runtime:** let openai and gemini agents actually call their tools ([08ff0b8](https://github.com/juancavallotti/octo/commit/08ff0b8b5aff281775933016542ff02d9e0c33cb))
* **runtime:** non-Anthropic agents answer in JSON where prose was asked for ([171031e](https://github.com/juancavallotti/octo/commit/171031e2a4cb4d58a32a8d99c5a2041627f30771))
* **runtime:** replay a gemini call id on the call as well as the response ([e45bfa5](https://github.com/juancavallotti/octo/commit/e45bfa5ceb7c8e7f079da0ad6f9e30021f954e04))
* **taskfile:** restart the log aggregator on a cluster deploy ([03c35b6](https://github.com/juancavallotti/octo/commit/03c35b628c61429438fa134e3844ebb59e6689e7))
* **taskfile:** stop hiding rollout restart failures behind || true ([2460d2f](https://github.com/juancavallotti/octo/commit/2460d2f2091e5a7361be363adc082afdf3b6a88d))
* **terraform:** pre-pull the digests the release actually deploys ([3bbf3b9](https://github.com/juancavallotti/octo/commit/3bbf3b9c27749fae213505d0f9023319dd4f3581))
* **terraform:** pre-pull the digests the release actually deploys ([72149ab](https://github.com/juancavallotti/octo/commit/72149ab8bc6a3e7478c6b196e981d27ad5186ecf))
* **terraform:** re-pull when the helper itself changes ([f3bc7b5](https://github.com/juancavallotti/octo/commit/f3bc7b5469d52a20974b4736f0d49f43db08080b))
* **terraform:** stage the octo-pull download outside /tmp ([f315d29](https://github.com/juancavallotti/octo/commit/f315d29b4a7a2f260327ac421e90ad07f78fc482))
* **terraform:** supply the chart's dev-run hash secret ([abc539e](https://github.com/juancavallotti/octo/commit/abc539e995d7b1ddf77931ae7a79051dc04bb3d6))
* **terraform:** supply the chart's dev-run hash secret ([3d338d4](https://github.com/juancavallotti/octo/commit/3d338d40f0e9b559a8944deba69f63b3271cb465))


### Performance

* **platform:** load the chat panel only when it is opened ([3dc36a4](https://github.com/juancavallotti/octo/commit/3dc36a45621825979d680f2618137b6be053ba02))


### Refactoring

* **orchestrator:** the AES-GCM cipher is not the kv package's ([0a9e715](https://github.com/juancavallotti/octo/commit/0a9e7151e5c828d12f9cce5572b6697a6bb6c909))
* **platform:** name the shared button for what it is ([25f7b74](https://github.com/juancavallotti/octo/commit/25f7b745a18c6ca1da168dd5e8d839ccc380deca))


### Documentation

* **agent:** the skills describe the answer slot they teach flows to use ([54370b3](https://github.com/juancavallotti/octo/commit/54370b3e58167c393f35ff44bc211867fa055a40))
* **platform:** data retention is built ([fac0260](https://github.com/juancavallotti/octo/commit/fac0260bc57c57df689106c239c4268565f13e66))
* **platform:** dr. octo reads history, not just live pods ([86ea661](https://github.com/juancavallotti/octo/commit/86ea6618731e439de826b2354affae3ad1aca5c0))
* **platform:** how to install, update, trace and edit dr. octo ([393104c](https://github.com/juancavallotti/octo/commit/393104c786b7569968be2d01aa5decdd18bdc748))
* **platform:** how to talk to dr. octo ([36a1f4e](https://github.com/juancavallotti/octo/commit/36a1f4eb864e23f7ff501ac97a6eac276d7b0c02))
* **platform:** list the agent module in the orchestrator's route table ([4b8c18b](https://github.com/juancavallotti/octo/commit/4b8c18b434c4e5f59f2971ba206f1f941580a500))
* **platform:** list the openapi module, and what still serves without a database ([1febc83](https://github.com/juancavallotti/octo/commit/1febc83d1c0f4075d8fb9d8a86f4a84358ecdb61))
* **platform:** name all three labels the one button takes ([c5192be](https://github.com/juancavallotti/octo/commit/c5192be29b846b0a44073b1dd34f6e8c817babce))
* **platform:** retention, and what a sweep takes with it ([4ff23fc](https://github.com/juancavallotti/octo/commit/4ff23fc1a32e6e84f1d72ee415694a6f5e9af672))
* **platform:** the agent ships traced, and why that is backwards on purpose ([68902df](https://github.com/juancavallotti/octo/commit/68902df1919ed72bdd6a72930d2480f345f37366))
* **platform:** the api description, and how to read part of it ([8f770d5](https://github.com/juancavallotti/octo/commit/8f770d563a9903e3e9dad24cdbb06fd08f204a17))
* **platform:** the llm key now has a consumer, and a provider switch clears it ([a4d7fc2](https://github.com/juancavallotti/octo/commit/a4d7fc2ac37921db28b79d135d5a8d2edf0e0f79))
* **platform:** the logs api describes itself ([78781de](https://github.com/juancavallotti/octo/commit/78781de1ebf89054efc83d6058972b5d09b835bb))
* **platform:** what a deployment may reach on the platform ([ccccd1e](https://github.com/juancavallotti/octo/commit/ccccd1ec2127979c88bcba65383f992001feb8d1))
* **platform:** what dr. octo remembers, and what that does not mean ([61a4a56](https://github.com/juancavallotti/octo/commit/61a4a569ff38cd7df7f2d403c5562f72ca126f0a))
* **platform:** what the observability grant does and does not widen ([071cf67](https://github.com/juancavallotti/octo/commit/071cf671d58417ac4988c2cd144e3eaf594ee7bf))
* **platform:** where the agent's edits go when you reinstall him ([b1c3929](https://github.com/juancavallotti/octo/commit/b1c392989ba5be5a18f4abc3444e6895d9337da9))

## [0.8.0](https://github.com/juancavallotti/octo/compare/v0.7.0...v0.8.0) (2026-08-10)


### ⚠ BREAKING CHANGES

* **helm:** orchestrator.devRuns.enabled now defaults to true, so the chart requires orchestrator.devRuns.hashSecret — it refuses to render without one, by design (rotating it breaks every webhook registered against an existing dev-run hostname). The cloud profiles (gke-autopilot, gke-standard, eks) inherit the new default and must now be given a hashSecret, alongside the baseDomain and clusterIssuer they already require. An install that does not want the editor's Run sets orchestrator.devRuns.enabled: false.
* **helm:** require a Postgres password instead of shipping one
* **helm:** stop defaulting the ingress class to traefik

### Features

* **ai-agent:** events route with streamed model output ([cacbe24](https://github.com/juancavallotti/octo/commit/cacbe24b5ec98e2d79f6a8d923465da554556199))
* **ai-agent:** stream agent events over SSE ([992db1f](https://github.com/juancavallotti/octo/commit/992db1f5571a0cff8c3593bd2d564af505d10d42))
* **ai:** trace every model call, not just the agent's turns ([af4f35e](https://github.com/juancavallotti/octo/commit/af4f35ec41a1bfb8d9631061ee0bcc741ad4454c))
* **ai:** trace every model turn ([adbabc6](https://github.com/juancavallotti/octo/commit/adbabc6c4c4f54f89b6bde2cf061285d8fdc4c91))
* **api:** fetch one trace's records ([5888330](https://github.com/juancavallotti/octo/commit/58883305e6b1e26e6853d4605db69991e6441c9f))
* **api:** list an app's traces with filters and keyset paging ([5fcf975](https://github.com/juancavallotti/octo/commit/5fcf97575757de81603e794e098e96e8db046d39))
* **api:** list the apps that have traces ([b504f13](https://github.com/juancavallotti/octo/commit/b504f13d9513d308dbb311ae8932539d710a9d41))
* **connectors:** trace source admission and response ([9572f2b](https://github.com/juancavallotti/octo/commit/9572f2ba7c658bb5e5a66abd0d746ddc65f63c77))
* **core:** expose the trace publisher as a runtime service ([811a543](https://github.com/juancavallotti/octo/commit/811a5431e64a666bc4f01c65c48ef2858eafa7e3))
* **core:** tell a block where it sits ([3d0ce08](https://github.com/juancavallotti/octo/commit/3d0ce08b4c8db4d26ced038d109f180b1844b602))
* **core:** the trace record, publisher and buffered driver ([14df221](https://github.com/juancavallotti/octo/commit/14df2219be3895e7a840a076564917d1983ab056))
* **cost:** keep price history in the database ([88cec04](https://github.com/juancavallotti/octo/commit/88cec04c251a00d0f37c72ff63c1372d1846075c))
* **cost:** price a model call from what the provider charged ([f7d9e45](https://github.com/juancavallotti/octo/commit/f7d9e459f7f07a94c59b58d9face85db816246c0))
* **cost:** read the public price catalogue ([b6470d8](https://github.com/juancavallotti/octo/commit/b6470d8f44517e08ab365cbd6ca7b5caa0269e11))
* **cost:** refresh prices on a schedule without locking readers ([8262de7](https://github.com/juancavallotti/octo/commit/8262de779f1dcde7d58621f356334037c13b2306))
* **cost:** resolve a model id to a rate ([3fc5d0b](https://github.com/juancavallotti/octo/commit/3fc5d0bb86377b37bfa5c36ad30a49f1242437b8))
* dev runs — move the editor's running app out of the BFF pod ([7fd706c](https://github.com/juancavallotti/octo/commit/7fd706cddb0709c3e64b001fe7670702423489bc))
* **dolphin:** let a case un-mock a block inherited from the file ([26b3009](https://github.com/juancavallotti/octo/commit/26b3009df21c4bafb37b1c47efabd0511b55cafb))
* **dolphin:** let a case un-mock a block inherited from the file ([f444e39](https://github.com/juancavallotti/octo/commit/f444e39df19a68b7d5e7f7ebf5145f959640ed15))
* **editor:** mongodb palette group and icon ([857dbb9](https://github.com/juancavallotti/octo/commit/857dbb9419ba8c47f33e0d32ceb2f504d7af2f82)), closes [#175](https://github.com/juancavallotti/octo/issues/175)
* **editor:** show a starting spinner while a run comes up ([6fb6386](https://github.com/juancavallotti/octo/commit/6fb638642b6f31876f4dd37b65e3ce763049f311))
* **editor:** un-mock a block for one case from the Testing tab ([b3a53b7](https://github.com/juancavallotti/octo/commit/b3a53b766d26b67d0dba05f12419725f8b30094e))
* Gateway API — emit HTTPRoute instead of Ingress ([282e2d2](https://github.com/juancavallotti/octo/commit/282e2d2a1a8790878d1a7908ad8b224caba12a80))
* **helm:** configure the trace consumer and the price refresh ([684b395](https://github.com/juancavallotti/octo/commit/684b395bd7d060be82d8420e33882cde3921b071))
* **helm:** default dev runs on for the PaaS chart ([8fa6bcb](https://github.com/juancavallotti/octo/commit/8fa6bcbd18671debec26c7c0fcf39d70863edfeb))
* **helm:** dev-run images and settings ([f7e90b2](https://github.com/juancavallotti/octo/commit/f7e90b2b62e7c6938129663da650592eddd3d8f9))
* **helm:** require a Postgres password instead of shipping one ([6a275d3](https://github.com/juancavallotti/octo/commit/6a275d337e4d59f9d39f2a269992dbf5e2bd139d)), closes [#203](https://github.com/juancavallotti/octo/issues/203)
* **helm:** route through Gateway API when networking.mode is gateway ([a3e2fe8](https://github.com/juancavallotti/octo/commit/a3e2fe8a12dfa74a440b7f0efa9e3b0b88dbd9be))
* **http:** server-sent events on the http route source ([6f1af17](https://github.com/juancavallotti/octo/commit/6f1af17945d63b007d33fb93f959f0e431c75757))
* **http:** sse-event block ([9c27b38](https://github.com/juancavallotti/octo/commit/9c27b38966dd2186658d9bc9fba4d4714905fa0d))
* **ingest:** consume internal.traces into the trace store ([ea694af](https://github.com/juancavallotti/octo/commit/ea694afee705b1aa559ada339cc8d88b6ca6a2a5))
* **ingest:** decode a trace record off the wire ([8b6b9ee](https://github.com/juancavallotti/octo/commit/8b6b9eeb6b99713dc81f66ec317fe0ceb11317d5))
* **ingest:** resolve a deployment to its integration ([1bc9d23](https://github.com/juancavallotti/octo/commit/1bc9d234b31143f9548bcd5ec3dee9400a3a5ebc))
* **k8s:** ship traces to the internal.traces subject ([f425cac](https://github.com/juancavallotti/octo/commit/f425caccf9fe3a9bf14d81c23ed823c6813a8b83))
* **kube:** run a deployment's pods with tracing on ([5b3913e](https://github.com/juancavallotti/octo/commit/5b3913e2f4f3ac4301262559465d022e46d02ea0))
* **llm:** report the model each turn was served by ([2aeca29](https://github.com/juancavallotti/octo/commit/2aeca295604c0d06eb0654de5c7af3f50db20ae2))
* **llm:** report what an embedding cost ([8d357e3](https://github.com/juancavallotti/octo/commit/8d357e325432f5376332cddb33c8b05ef2375c94))
* **llm:** streaming, thinking, and token usage across the three providers ([335a527](https://github.com/juancavallotti/octo/commit/335a52743f0b94497ab88b12a98ee1030609257f))
* **logs:** price a call from the provider it reported, not the rate that matched ([eeadc94](https://github.com/juancavallotti/octo/commit/eeadc94f6d7ba26310a19a2fe6526c1ef7c2133b)), closes [#254](https://github.com/juancavallotti/octo/issues/254)
* **logs:** price cache writes at the published write rate ([8feaec8](https://github.com/juancavallotti/octo/commit/8feaec8541e7817677db419a3b7f62585575d54d)), closes [#253](https://github.com/juancavallotti/octo/issues/253)
* **logs:** serve traces alongside logs ([028f99e](https://github.com/juancavallotti/octo/commit/028f99e535919f04f84ef9b08f8c5b717cfdbc17))
* **logs:** store the cache-write count beside the other three ([8c16ae3](https://github.com/juancavallotti/octo/commit/8c16ae3a4aafd0a0cb1ed3ea3cd1e6b88c0cf24f)), closes [#253](https://github.com/juancavallotti/octo/issues/253)
* **mcp:** accept a null mock on set_test_case ([231c691](https://github.com/juancavallotti/octo/commit/231c6917f9d6268c23cc8bf16bf63559d3cf1337))
* **mongodb:** a MongoDB connector with find/insert/update/delete/aggregate blocks ([2eb1de9](https://github.com/juancavallotti/octo/commit/2eb1de96208c9ea8f99740b1041badfcb8a1afc8))
* **mongodb:** add the aggregate block ([836bf94](https://github.com/juancavallotti/octo/commit/836bf94888792c57dc46890ae34520b45f5c360b)), closes [#175](https://github.com/juancavallotti/octo/issues/175)
* **mongodb:** add the connector ([0de2069](https://github.com/juancavallotti/octo/commit/0de2069b6de32c1586466235ae541d305c983b17)), closes [#175](https://github.com/juancavallotti/octo/issues/175)
* **mongodb:** add the delete block ([b4caf45](https://github.com/juancavallotti/octo/commit/b4caf45842ea36920646f9fa83b872b1faf3b886)), closes [#175](https://github.com/juancavallotti/octo/issues/175)
* **mongodb:** add the find block ([433c2d4](https://github.com/juancavallotti/octo/commit/433c2d4a520c52ae04bb5d64c57a21005fb5c278)), closes [#175](https://github.com/juancavallotti/octo/issues/175)
* **mongodb:** add the insert block ([1d811e7](https://github.com/juancavallotti/octo/commit/1d811e7932e8e7b6b5f661aef0b6d1f23556877c)), closes [#175](https://github.com/juancavallotti/octo/issues/175)
* **mongodb:** add the update block ([0e3cc3c](https://github.com/juancavallotti/octo/commit/0e3cc3c222c9fcd97446972fdfcd073224d98565)), closes [#175](https://github.com/juancavallotti/octo/issues/175)
* **mongodb:** bound an aggregation with a limit ([160353a](https://github.com/juancavallotti/octo/commit/160353aa26b9cb041bf85dbb988aa906b0a149c9))
* **mongodb:** make the startup ping optional ([a11ffac](https://github.com/juancavallotti/octo/commit/a11ffac1a7c08eca290e11654d978ae5883b8d0e)), closes [#175](https://github.com/juancavallotti/octo/issues/175)
* **mongodb:** the extended JSON bridge and block helpers ([cb1a098](https://github.com/juancavallotti/octo/commit/cb1a0984c570f4ed87b40253813cd48f6ef196f2)), closes [#175](https://github.com/juancavallotti/octo/issues/175)
* **octo:** wire --traces into the runtime CLI ([8e6edd6](https://github.com/juancavallotti/octo/commit/8e6edd65bc17455678f439725b723af4f006407d))
* **orchestrator:** attach imagePullSecrets to integration pods ([d98990f](https://github.com/juancavallotti/octo/commit/d98990f83524fc3a3c317988f05ba0064f5f2c0e)), closes [#203](https://github.com/juancavallotti/octo/issues/203)
* **orchestrator:** dev-run API and wiring ([87dc736](https://github.com/juancavallotti/octo/commit/87dc736e2402657eed19508fa7cccc7856dfa4d0))
* **orchestrator:** dev-run identity and lifecycle service ([3b12b43](https://github.com/juancavallotti/octo/commit/3b12b439cda4a9f8b7e94502b340c09288d43254))
* **orchestrator:** dev-run pod spec and status ([a2c783d](https://github.com/juancavallotti/octo/commit/a2c783d159135cc9167cdb275d1b343740e0cc8e))
* **orchestrator:** make tracing a per-deployment setting ([5761bb1](https://github.com/juancavallotti/octo/commit/5761bb1ce39e3353d44d4c7b0c1f72baa18fef54))
* **orchestrator:** publish external endpoints through Ingress or Gateway API ([06c64dc](https://github.com/juancavallotti/octo/commit/06c64dc2ae980e538194210963ddeca5d6e4a51d))
* **platform:** classify what a trace's time was spent on ([7b11d15](https://github.com/juancavallotti/octo/commit/7b11d15581fd336a6fb9e9cca3041ae682c2056b))
* **platform:** MCP runs apps as dev runs ([f929024](https://github.com/juancavallotti/octo/commit/f92902430aab98aec55df4b75a0cf57878659ce4))
* **platform:** one app's traces, filtered and paged ([b00a80d](https://github.com/juancavallotti/octo/commit/b00a80d50751b4e641180156f41d68b5fd22bce2))
* **platform:** open a span and see what it carried ([aa0027f](https://github.com/juancavallotti/octo/commit/aa0027f9dcefe479a9328eb56b136787bc5acebf))
* **platform:** read a block's address out of a trace record ([62982b5](https://github.com/juancavallotti/octo/commit/62982b5060728e7a2fb024d9518f6d6ea166b9cc))
* **platform:** rebuild a trace into a waterfall ([a8fb1a6](https://github.com/juancavallotti/octo/commit/a8fb1a6f8786ac391fe9d0324486b04ad6a0ae9e))
* **platform:** run apps as dev runs ([3ff6176](https://github.com/juancavallotti/octo/commit/3ff617679ed603763c74025059d76f59fc7eb43e))
* **platform:** show cache writes in the record inspector ([912cb03](https://github.com/juancavallotti/octo/commit/912cb03f70f051740dcfaadb57b72d9e30eeeb96)), closes [#253](https://github.com/juancavallotti/octo/issues/253)
* **platform:** switch tracing on from the deploy and rollout dialogs ([69e9e9f](https://github.com/juancavallotti/octo/commit/69e9e9fdd2382ce4d97d80d6ee8feda0f9de08a7))
* **platform:** the Traces route and the apps that have traces ([2e1c6f9](https://github.com/juancavallotti/octo/commit/2e1c6f9b78365a663a03dac6166454e2bd5fe8ea))
* **platform:** the traces service client ([e986693](https://github.com/juancavallotti/octo/commit/e9866934e3575c0323e1c17d80947c3549aeb212))
* **platform:** the Traces tab — app list, trace list, waterfall, summary ([7998d6b](https://github.com/juancavallotti/octo/commit/7998d6b35f5d1dbbb503762b75c660840566d8b5))
* **platform:** the waterfall ([8bfb529](https://github.com/juancavallotti/octo/commit/8bfb529600571ecfcd249d4450bf061b6247eaa1))
* **platform:** what a trace adds up to ([ab0f326](https://github.com/juancavallotti/octo/commit/ab0f326610e443080a7a9248efb8217a5e6a64c5))
* price the cache-write tokens Anthropic reports ([652df33](https://github.com/juancavallotti/octo/commit/652df33654f024a1802d6c36f6d1dceda0ce4d5a))
* **repo:** batch-insert trace records and roll their traces up ([c77c54a](https://github.com/juancavallotti/octo/commit/c77c54a97b6f5960c39d2643bf9d780cc917171a))
* **runtime:** a connector can name its vendor family ([7c11c8e](https://github.com/juancavallotti/octo/commit/7c11c8eda989821db1616390f73a69ee9057c5d6)), closes [#254](https://github.com/juancavallotti/octo/issues/254)
* **runtime:** carry Anthropic's cache-write token count ([0ebdfa8](https://github.com/juancavallotti/octo/commit/0ebdfa8c106424f3883e739ae2d5328c08dfa96c)), closes [#253](https://github.com/juancavallotti/octo/issues/253)
* **runtime:** llm.turn and llm.embed carry the provider ([9ccf3ef](https://github.com/juancavallotti/octo/commit/9ccf3efd1f20a53415d3c7d5139b36523bb49743)), closes [#254](https://github.com/juancavallotti/octo/issues/254)
* **runtime:** mint a trace id at the flow boundary and carry it across joins ([21680fc](https://github.com/juancavallotti/octo/commit/21680fc16a58bf6d565bdb4bf8a4968b131ddf0a))
* **samples:** mongodb orders sample ([b99320d](https://github.com/juancavallotti/octo/commit/b99320d9609fd4120635c790f606ca85d5899a2f)), closes [#175](https://github.com/juancavallotti/octo/issues/175)
* **services:** the tracing hosted service ([ff7aee6](https://github.com/juancavallotti/octo/commit/ff7aee6122c8847b1c8dd5dcd576e198e4007e5a))
* **sidecars:** dev-run workspace management ([b24b25a](https://github.com/juancavallotti/octo/commit/b24b25aed9fd3d6edea031f47a26a309ede3b327))
* **sidecars:** new module and dev sidecar scaffold ([1c5dc71](https://github.com/juancavallotti/octo/commit/1c5dc718e8660f20b4f9e42cbfb7518d6be54338))
* **sidecars:** orchestrator bundle client and reload coordination ([165d9d1](https://github.com/juancavallotti/octo/commit/165d9d11f61896652a059a9ef090975676dad7aa))
* **sidecars:** reload command and diagnostics API ([6f3155a](https://github.com/juancavallotti/octo/commit/6f3155a82a0a9cd7763267a6bb3b49fcf3b659cf))
* **sql:** the trace, summary and LLM price tables ([74d0ac7](https://github.com/juancavallotti/octo/commit/74d0ac70e66254ba2acb780fdf41e10ecde9d97a))
* **standalone:** write traces to a JSONL file ([05173bd](https://github.com/juancavallotti/octo/commit/05173bd0444928fcb1b6eef79dd662e82a3266ab))
* **types:** add the llm.embed trace kind ([af31349](https://github.com/juancavallotti/octo/commit/af31349be7fbbd823a26ef6311e0cd93f94a6612))


### Bug Fixes

* address CodeRabbit review on the Gateway API work ([4056935](https://github.com/juancavallotti/octo/commit/4056935434ba473ace4488b85ab733b9df666073))
* **ai:** keep a stopped run's memory replayable, and correct the streaming docs ([5fd2c2a](https://github.com/juancavallotti/octo/commit/5fd2c2aa0313616335110924407f8b155ed0e106))
* **api:** reject a backwards from/to window on the trace list ([6745768](https://github.com/juancavallotti/octo/commit/67457682b7eb6e36e5119e7fe252fe647769ab8e))
* **bundle:** keep the Expire error body for its snippet ([483e8a1](https://github.com/juancavallotti/octo/commit/483e8a18878d8f9a7f9b0800c5d3bfd7c619a864))
* **ci:** drop the NOTES dry run — helm lint already renders it ([10e9472](https://github.com/juancavallotti/octo/commit/10e94722abad7e972e9ef760ad13b98d84468433)), closes [#202](https://github.com/juancavallotti/octo/issues/202)
* **ci:** stamp every chart version, and stop eating the comments ([74e1d69](https://github.com/juancavallotti/octo/commit/74e1d69485c7c7e4d05f5d66365a6c3f297efd37))
* **ci:** stamp every chart version, and stop eating the comments ([c6578df](https://github.com/juancavallotti/octo/commit/c6578df2cd835c6e023c27469bd4064784fceb7d))
* **core:** announce dropped trace records at shutdown ([756a533](https://github.com/juancavallotti/octo/commit/756a533e6ef5bc1cbbb5568d78e7d797aac5b764))
* **cost:** record a refresh outcome that shutdown interrupted ([086b153](https://github.com/juancavallotti/octo/commit/086b153506a7a45ef40e741dde569f790d71a715))
* **devrun:** detach and bound the save-triggered reload fan-out ([6e2b5fd](https://github.com/juancavallotti/octo/commit/6e2b5fd3492dd026e6d12a2b8be4bfb5511cc822))
* **devrun:** keep the dev-env resource last so it always wins ([1dc758a](https://github.com/juancavallotti/octo/commit/1dc758ab4a4f4500e76fd1f2c99467549a8eee85))
* **devrun:** never reap a dev run with no timestamps ([4ea9275](https://github.com/juancavallotti/octo/commit/4ea9275f02240dbe8b8fb93ea6e6c70ddff823f5))
* **devspace:** put the deployment namespace where v2beta1 expects it ([137f5f6](https://github.com/juancavallotti/octo/commit/137f5f689245e415b1f939ba3d47ac7c7d63335f))
* **editor:** do not offer a file-level null as a mock to lift ([51badef](https://github.com/juancavallotti/octo/commit/51badeff1568fdabd3ae4d0f86575ee46de18ec5))
* **editor:** don't crash the console panel when localStorage is unavailable ([b03555c](https://github.com/juancavallotti/octo/commit/b03555c2d8d13563f0b9afa2628ebdccfcb9377c))
* **editor:** reset the run when the open integration changes ([eb297ee](https://github.com/juancavallotti/octo/commit/eb297eea6c3b427b7507438e43e0a8b54d6ffd36))
* **editor:** reveal a starting dev run's public URL without a reload ([1e5c84d](https://github.com/juancavallotti/octo/commit/1e5c84d60798cd9afa65bb56322ac2d2636d9963))
* **helm:** address review — TLS-none scheme, secret docs, nil assertion ([1748b55](https://github.com/juancavallotti/octo/commit/1748b550e9ffe4fe9ae6e217884082e15a83e52e))
* **helm:** let a wildcard certificate be referenced, not only issued ([298aeba](https://github.com/juancavallotti/octo/commit/298aeba3d8326f9a22c4ed054f70fd7a49c9a3bf))
* **helm:** make NOTES.txt tell the truth about TLS ([2bac106](https://github.com/juancavallotti/octo/commit/2bac1061d39bccb69be5571cadf6ba47b84e7f06)), closes [#202](https://github.com/juancavallotti/octo/issues/202)
* **helm:** stop defaulting the ingress class to traefik ([ed1649c](https://github.com/juancavallotti/octo/commit/ed1649c218cc8a96b9225326fe7e9d338a24acb9)), closes [#202](https://github.com/juancavallotti/octo/issues/202)
* **http-client:** a failed REST call names the URL it actually requested ([a0484e0](https://github.com/juancavallotti/octo/commit/a0484e0aa94b519cde0a2223a93bc7a3eaf03524)), closes [#264](https://github.com/juancavallotti/octo/issues/264)
* **http-client:** a resolved request target is always an absolute path ([ce6ddf5](https://github.com/juancavallotti/octo/commit/ce6ddf510c9f5e170fb88bb876ad86fe8c293656)), closes [#264](https://github.com/juancavallotti/octo/issues/264)
* **http-client:** drop userinfo entirely from a reported URL ([716b52f](https://github.com/juancavallotti/octo/commit/716b52fdb7a0084fd2a560033a981bb603df104d))
* **http-client:** resolve a relative request target against the base URL ([25735b1](https://github.com/juancavallotti/octo/commit/25735b19e9000282abed78eb08c7491bdea7d93a))
* **http:** fail an address that names no stream, and stop recommending tokens in URLs ([7924189](https://github.com/juancavallotti/octo/commit/7924189debb1ac91bcabceb05731896eb9a35e21))
* **http:** keep render failures off the ifClosed path, and claim the response atomically ([26eb56c](https://github.com/juancavallotti/octo/commit/26eb56c44f10c9589480615e95a488e2d5d2f455))
* **http:** never throw on a null or non-object failure body ([0f0315b](https://github.com/juancavallotti/octo/commit/0f0315bb6c7dd98d54b7f879df580490889830e2))
* **httpx:** flush the stream header before the first body byte ([ac63cf6](https://github.com/juancavallotti/octo/commit/ac63cf685c892518241abe53afaa5aca7834ffa6))
* **ingest:** don't write a trace batch under a cancelled context ([2c03de5](https://github.com/juancavallotti/octo/commit/2c03de59d19bcc21d608de118f6e1248094dc8d9))
* **llm:** re-check the thinking budget against the request that is being sent ([fbbbed3](https://github.com/juancavallotti/octo/commit/fbbbed3b523922bbc06d58bde87afe4c9a7a966f))
* **logs:** a blank reported provider falls back rather than counting as one ([24e2638](https://github.com/juancavallotti/octo/commit/24e2638e8dcc389c46537086864ee1c9d4184ca1))
* **logs:** a log page boundary neither skips nor repeats a tied timestamp ([f88857b](https://github.com/juancavallotti/octo/commit/f88857b6d52d06d91029d9329d4f2fd4cfa6db95)), closes [#257](https://github.com/juancavallotti/octo/issues/257)
* **logs:** bound a shutdown by the flush budget, not by a single write's timeout ([535aed6](https://github.com/juancavallotti/octo/commit/535aed6a6972262668def51165e8fceccd1421b2))
* **logs:** break log paging ties on id ([3a8ff85](https://github.com/juancavallotti/octo/commit/3a8ff85fda7409643a0e3fc25fbfa59076a379e0))
* **logs:** close the gaps a review found ([5bce1e9](https://github.com/juancavallotti/octo/commit/5bce1e946195a53e7d5502d7462aca6afd24b2f4))
* **logs:** shed log records instead of blocking the NATS callback ([304f2c1](https://github.com/juancavallotti/octo/commit/304f2c1392a5fd801247fb65a12e7ea888f7009e)), closes [#261](https://github.com/juancavallotti/octo/issues/261)
* **logs:** stop losing log records on shutdown and under load ([7fcd4ba](https://github.com/juancavallotti/octo/commit/7fcd4babc03dd84c8d35d079960734a2e886acb5))
* **logs:** write the log records already taken off the wire before shutting down ([59fe541](https://github.com/juancavallotti/octo/commit/59fe5413e22c419968c024586074130e20810050)), closes [#260](https://github.com/juancavallotti/octo/issues/260)
* **mcp:** don't gate a run on the local one-shot binary ([963759d](https://github.com/juancavallotti/octo/commit/963759dc26ceca0ec1c659416f8492a0dd7202de))
* **mongodb:** match the connection-string scheme exactly ([fb2454f](https://github.com/juancavallotti/octo/commit/fb2454f05f9195407c653d8772d74ed731fbeae1))
* **orchestrator:** a rollout that cannot record its settings must not report success ([01bcc86](https://github.com/juancavallotti/octo/commit/01bcc868aa552c4918de8c3815c9102f522fd3d6))
* **orchestrator:** a rollout that cannot record itself must not report success ([adf38bc](https://github.com/juancavallotti/octo/commit/adf38bc374e622f1945faf2e054921e9562f6a68)), closes [#265](https://github.com/juancavallotti/octo/issues/265)
* **orchestrator:** check what the tests close ([deabf31](https://github.com/juancavallotti/octo/commit/deabf31f900886a625f88798a46b919261b2cd09))
* **orchestrator:** clear the lint findings and run the module in CI ([73c3f7b](https://github.com/juancavallotti/octo/commit/73c3f7b7a2c6ea9c94c471d1b57e03719d0bb23a))
* **orchestrator:** convert the wire structs, and drop the embedded selectors ([b739e44](https://github.com/juancavallotti/octo/commit/b739e447077fe7a32f0764e013e9c59f221562a4))
* **orchestrator:** gofmt the fakeRepo struct ([874fb4b](https://github.com/juancavallotti/octo/commit/874fb4bd75e6e46f4c20e0973bd0d9f59afa34c4))
* **orchestrator:** gofmt the fakeRepo struct ([4ea1c69](https://github.com/juancavallotti/octo/commit/4ea1c69adb34da3d55c3cd73d13e4507ff9bbd8d))
* **platform:** CodeRabbit findings on the Traces tab ([44ea2c1](https://github.com/juancavallotti/octo/commit/44ea2c1bb7b81a0e86f7678029ad3099d35b061e))
* **platform:** don't abort the trace queries by navigating on mount ([4cd8aa9](https://github.com/juancavallotti/octo/commit/4cd8aa9e2904539fc75ced154e4080354eb3da1f))
* **platform:** mark a timed-out log read as truncated ([f00be79](https://github.com/juancavallotti/octo/commit/f00be791ff15f4bb68ead1f5e5139390420bd3cc))
* **reload:** detach coalesced rounds from the request context ([5c2994e](https://github.com/juancavallotti/octo/commit/5c2994e37ec8a3b0dc12182086d80484b5d4af6e))
* **run-host:** clamp a caller's test timeout and parallelism ([874c78d](https://github.com/juancavallotti/octo/commit/874c78de871dd1fd1eeb6d6a16fda59931dd6c3e))
* **run-host:** pin the octo and TMPDIR invariants past the caller's env ([d1b3222](https://github.com/juancavallotti/octo/commit/d1b322275745d8c1a7207a2776cc11a8c441ff6e))
* **run-host:** stage every test suite to a unique path ([195e237](https://github.com/juancavallotti/octo/commit/195e23708d178752ebb0de06a6bda9d0f93b020f))
* **run-host:** validate the eval envelope shape before trusting ok ([3c146a1](https://github.com/juancavallotti/octo/commit/3c146a1e070e808a8bae55106466f11e2ac3acc1))
* **run-host:** validate the report's shape before mapping it ([2ec694d](https://github.com/juancavallotti/octo/commit/2ec694d38bd6f25cc188a8ee079fcf326cf76ef7))
* **runtime:** report a failure to close the trace sink ([c6aafe3](https://github.com/juancavallotti/octo/commit/c6aafe3348cc09d5c32f880fff1271006adcc73d))
* **services:** register the trace listeners exactly once ([52fbf2b](https://github.com/juancavallotti/octo/commit/52fbf2b31b9bbef508a89d5d0c92232d3b120554))
* **sidecar:** bound request body, response, and idle connections ([974401c](https://github.com/juancavallotti/octo/commit/974401c2b39d95a76b553afc0001f347afd18df5))
* **standalone:** do not fsync a trace pipe ([02f58b4](https://github.com/juancavallotti/octo/commit/02f58b49f8e2b4c36579d6d44e25af36520809b1))
* **standalone:** don't let a failed reaper sweep crash the process ([96851a8](https://github.com/juancavallotti/octo/commit/96851a82c5c59e8d51f696cc969144e69e2dacd5))
* **standalone:** harden the log pipe against errors and runaway lines ([415e7cf](https://github.com/juancavallotti/octo/commit/415e7cf13f61de790ca88b20cefef0f43697bce7))
* **standalone:** narrow a trace file that others can already read ([3df6f1b](https://github.com/juancavallotti/octo/commit/3df6f1b2ff9813d568c7d33d59c8dd04c227a9a5))
* **standalone:** parse the HTTP_PORT default as strictly as the orchestrator ([65edb7c](https://github.com/juancavallotti/octo/commit/65edb7c8de8d3ed0c9eab23e16dbb8c6a037476e))
* **standalone:** serialize a namespace's start, stop and sync ([ef9827c](https://github.com/juancavallotti/octo/commit/ef9827cf8572556d3af035c5f1230017aa92315a))
* **workspace:** reject resources that would merge into the runtime config ([dec21eb](https://github.com/juancavallotti/octo/commit/dec21eb87db5ce1ebbf1f66bc6417c54f5f05d71))


### Refactoring

* **api:** name each handler for the stream it serves ([63e4734](https://github.com/juancavallotti/octo/commit/63e4734f94d9ccb414319825b5c763673d861209))
* **logs:** one keyset cursor codec for both list endpoints ([5aab081](https://github.com/juancavallotti/octo/commit/5aab08106e4e5abe49f46deaaad207283196f064)), closes [#257](https://github.com/juancavallotti/octo/issues/257)
* **logs:** one shedding policy for both ingest consumers ([2670042](https://github.com/juancavallotti/octo/commit/2670042f69f8be9caa0d655fc0436a02f7a20b70)), closes [#261](https://github.com/juancavallotti/octo/issues/261)
* **mcp:** the run host is injected, not assumed ([db0e7dd](https://github.com/juancavallotti/octo/commit/db0e7dd6993a1374ed05930d57a5bfc58e6ea12b))
* **orchestrator:** give kube.New a Config struct ([6503a85](https://github.com/juancavallotti/octo/commit/6503a85def731b60ac022363c7f1bed8f0f32ae6)), closes [#203](https://github.com/juancavallotti/octo/issues/203)
* **platform:** a deployment row's parts are its parts ([57697b9](https://github.com/juancavallotti/octo/commit/57697b9ce7aee6ac185de85538a18befc4868c5b)), closes [#268](https://github.com/juancavallotti/octo/issues/268)
* **platform:** a folder row renders itself ([25f31f8](https://github.com/juancavallotti/octo/commit/25f31f81c8455d605b3e438b144a6cc7c72e06d4)), closes [#268](https://github.com/juancavallotti/octo/issues/268)
* **platform:** a resource's display shape is pure ([7483f38](https://github.com/juancavallotti/octo/commit/7483f38defdc5d96beeadebdcf88bc2ecc14aa8b)), closes [#268](https://github.com/juancavallotti/octo/issues/268)
* **platform:** both deploy dialogs share their input styling and options fetch ([5c1a991](https://github.com/juancavallotti/octo/commit/5c1a991e08e2e3b77cdcbf55ab4e45ff4f01cab3)), closes [#268](https://github.com/juancavallotti/octo/issues/268)
* **platform:** one relativeAge, not four ([6e6c020](https://github.com/juancavallotti/octo/commit/6e6c02047946b6a0d23bba9b8db9192a1987a7d5)), closes [#268](https://github.com/juancavallotti/octo/issues/268)
* **platform:** resolving a drop is a pure function ([b5383ba](https://github.com/juancavallotti/octo/commit/b5383ba9a9dbea090e88c39bf6f834831b3acb9a)), closes [#268](https://github.com/juancavallotti/octo/issues/268)
* **platform:** split the objects browser, and gate lint at zero ([97ba7d4](https://github.com/juancavallotti/octo/commit/97ba7d4489cee0d9ee0967fdd5fabf2a08d01536))
* **platform:** the BFF client is a transport plus three resource groups ([d3a3041](https://github.com/juancavallotti/octo/commit/d3a304145d4bed7dbd86879e2059752b2ee22fc9)), closes [#268](https://github.com/juancavallotti/octo/issues/268)
* **platform:** the deploy form's fields are their own component ([9c89918](https://github.com/juancavallotti/octo/commit/9c899180c90f76142d4229adb187b230d0bc30bc)), closes [#268](https://github.com/juancavallotti/octo/issues/268)
* **platform:** the deployments panel's data is a hook ([2776103](https://github.com/juancavallotti/octo/commit/2776103d50f980df866be240d37ec2d850cd6ee1)), closes [#268](https://github.com/juancavallotti/octo/issues/268)
* **platform:** the integration header owns its own rename ([1c33569](https://github.com/juancavallotti/octo/commit/1c33569ea042c8cbe11e84a30e4399ec795cf3ab)), closes [#268](https://github.com/juancavallotti/octo/issues/268)
* **platform:** the integration tree's data and mutations are hooks ([4cfc0bb](https://github.com/juancavallotti/octo/commit/4cfc0bbbedc4e67143f8c140ad6dec73615a0dd7)), closes [#268](https://github.com/juancavallotti/octo/issues/268)
* **platform:** the manager's toolbar and unavailable state are components ([0b9ad18](https://github.com/juancavallotti/octo/commit/0b9ad1885f85469c93c407ed5ab7fed6387c145f)), closes [#268](https://github.com/juancavallotti/octo/issues/268)
* **platform:** the objects browser's lifecycle is a hook ([d7c02e6](https://github.com/juancavallotti/octo/commit/d7c02e6614e32fd940f101dd2bee928df44e5a89)), closes [#268](https://github.com/juancavallotti/octo/issues/268)
* **platform:** the orchestrator model's types are their own module ([080275e](https://github.com/juancavallotti/octo/commit/080275e147d131345fa09574ace42ff076e43a3e)), closes [#268](https://github.com/juancavallotti/octo/issues/268)
* **platform:** the rollout form is its own file ([1921f6c](https://github.com/juancavallotti/octo/commit/1921f6cfdc924f423c8221ccee710794adb3abb3)), closes [#268](https://github.com/juancavallotti/octo/issues/268)
* **platform:** the run actions' helpers are not server actions ([eed3b3c](https://github.com/juancavallotti/octo/commit/eed3b3c1273ad98aaaecae1f574ce47553b7ff76)), closes [#268](https://github.com/juancavallotti/octo/issues/268)
* **platform:** writing an object is its own hook ([6ff06ed](https://github.com/juancavallotti/octo/commit/6ff06ed5345efc5632d8485dae4234ae54423730)), closes [#268](https://github.com/juancavallotti/octo/issues/268)
* **repo,ingest:** name each type for the stream it handles ([acee3ff](https://github.com/juancavallotti/octo/commit/acee3ff01502487d668f1773c51c95ef495403d9))
* **run-host:** a backend reports the run, the host reports its binaries ([b164aaf](https://github.com/juancavallotti/octo/commit/b164aaf8e8f36ab030a4f6ec7634629aef3b1707))
* **run-host:** split the app runner from execution ([e353a89](https://github.com/juancavallotti/octo/commit/e353a89fa2006f050d725ba4d2757a405c674585))
* **standalone:** lint to zero, and run the app in CI ([a710ce5](https://github.com/juancavallotti/octo/commit/a710ce5662da048b794cae81e15ad34029c02db3))
* **standalone:** own the local app runner ([3d47cb9](https://github.com/juancavallotti/octo/commit/3d47cb91e7cc74adc9bb2004f7a525e55672087e))
* **standalone:** separate what a run is from what starts one ([704bad1](https://github.com/juancavallotti/octo/commit/704bad1b07661fda52c9a32dc9b5324cca13ce77))


### Documentation

* a model call records the provider that served it ([56ec06c](https://github.com/juancavallotti/octo/commit/56ec06c0aa8b89334787c0b094335cd2323fe44c)), closes [#254](https://github.com/juancavallotti/octo/issues/254)
* **ai:** sample streaming an agent's events over SSE ([a9fea49](https://github.com/juancavallotti/octo/commit/a9fea4991f8e0089f0d66b9f515727fe7fb62fc2))
* cache writes are captured and priced ([0f614ba](https://github.com/juancavallotti/octo/commit/0f614ba10aaffa6b75b591cae753df019c11e27c)), closes [#253](https://github.com/juancavallotti/octo/issues/253)
* dev runs ([593475d](https://github.com/juancavallotti/octo/commit/593475d3720d071c5e5d97a7f5985fbf0f38e28f))
* **helm:** LOGS_URL serves traces too, not just logs ([d10b711](https://github.com/juancavallotti/octo/commit/d10b711695ef74628275c2040343c754c0096038))
* **helm:** name the things the chart deliberately does not create ([85274a0](https://github.com/juancavallotti/octo/commit/85274a04cd67b2b2762ac8388ac03e5c057f1741)), closes [#203](https://github.com/juancavallotti/octo/issues/203)
* **helm:** the Gateway API routing mode ([be19282](https://github.com/juancavallotti/octo/commit/be1928271f11618aa646fe9460392b591c01bd6a))
* **helm:** what a mode switch leaves behind ([970000d](https://github.com/juancavallotti/octo/commit/970000d96d35c857fd9dd2ee4efb7cd82d59c9c4))
* **helm:** worked values.yaml per TLS mode, and where certificates come from ([083957f](https://github.com/juancavallotti/octo/commit/083957fd0dfca33101d7fdc7b86b47d19cadf037))
* **http-client:** a path joins the base URL, slash or no slash ([e4839ae](https://github.com/juancavallotti/octo/commit/e4839ae7595c18abe777846b273b55976f2cdc62)), closes [#264](https://github.com/juancavallotti/octo/issues/264)
* **http:** document server-sent events, with a sample and its suite ([e257db3](https://github.com/juancavallotti/octo/commit/e257db3d4dc6d3f35374258938f0ea563e7c8e62))
* **lint:** say which linter set runs where, and why ([183d759](https://github.com/juancavallotti/octo/commit/183d759fe13dea3aae8c7f06ecf438b08f3f6681))
* **logs:** correct three claims about how the consumer behaves ([909b9f1](https://github.com/juancavallotti/octo/commit/909b9f13f8958e4abc08a31d35966539ba670721))
* **logs:** next_before is an opaque cursor, not a timestamp ([b29fc75](https://github.com/juancavallotti/octo/commit/b29fc75574e663c10c04fad90449fa1fe92b00cb)), closes [#257](https://github.com/juancavallotti/octo/issues/257)
* **logs:** say what a NULL cache_write_tokens actually means ([3f468ef](https://github.com/juancavallotti/octo/commit/3f468efbe56a902c444b1bf31039095da7913ecf))
* **mongodb:** fix the delete example and the guide's env block ([7a2b909](https://github.com/juancavallotti/octo/commit/7a2b9093994392dea134749f6838f29952d43ab7))
* **platform:** a rollout that cannot record itself fails ([dfd7fa4](https://github.com/juancavallotti/octo/commit/dfd7fa4806708d6156629eeac6f27fd279198468)), closes [#265](https://github.com/juancavallotti/octo/issues/265)
* **platform:** name the telemetry aggregator once, and say when a call is unpriced ([3b20d99](https://github.com/juancavallotti/octo/commit/3b20d998d1a677c52cfb3fec2d8bc39419dded31))
* **platform:** the trace store and how model calls are priced ([60371c4](https://github.com/juancavallotti/octo/commit/60371c48642efd067212b5e30c47010de6dd195a))
* **platform:** tracing is a troubleshooting tool, not a production setting ([2c5bc93](https://github.com/juancavallotti/octo/commit/2c5bc93d14217c685f5f7b5d30f0d2405d9ce597))
* **run-host:** document the eval argv exposure and its threat model ([01f8cb7](https://github.com/juancavallotti/octo/commit/01f8cb7aeb2894e182644d66591d282ed49a1208))
* **runtime:** count embeddings in the cost query ([4d4ed49](https://github.com/juancavallotti/octo/commit/4d4ed49497dc56969fb85ce299e8fc5501d38dc9))
* **runtime:** record what every model call cost ([ae2de9b](https://github.com/juancavallotti/octo/commit/ae2de9badb92a1b3911194625720317401b36a9a))
* **runtime:** what the log pipeline does under pressure ([c63bc75](https://github.com/juancavallotti/octo/commit/c63bc7526250645f5aa959959852d9dc286d4f34))
* stop describing the ingress as Traefik outside the GCP guide ([7705f97](https://github.com/juancavallotti/octo/commit/7705f974dee19bdc9cb666ee971ba05534833d08)), closes [#202](https://github.com/juancavallotti/octo/issues/202)
* **testing:** document un-mocking, and give the SSE suite its file-level mocks back ([c0051ee](https://github.com/juancavallotti/octo/commit/c0051eed70e56a603c5cd582de24b112676dfc26))

## [0.7.0](https://github.com/juancavallotti/octo/compare/v0.6.4...v0.7.0) (2026-08-03)


### ⚠ BREAKING CHANGES

* **deploy:** deploy/terraform/octo.tfvars is replaced by a terraform.tfvars in each root, and `terraform init` now needs -backend-config=../backend.hcl. Copy the new *.example files and re-run `terraform init` before applying.

### Features

* **deploy:** configure editor SSO and mint app secrets in the cluster roots ([053d3cc](https://github.com/juancavallotti/octo/commit/053d3cc551ea38543338887001e1aff83737c8bc))
* **deploy:** deploy the images a build produced, by digest ([cd23f63](https://github.com/juancavallotti/octo/commit/cd23f634eb6e810f979cf0db8512b2923cf00ff4))
* **deploy:** EKS root with VPC, ALB controller, ACM and RDS ([faa8723](https://github.com/juancavallotti/octo/commit/faa8723e2e306b389708aded2a4b6d24e4340f07))
* **deploy:** give the k3s VM a dedicated data disk for Postgres ([408c8c2](https://github.com/juancavallotti/octo/commit/408c8c29944b589b4aa22b8d45d88520252fd2b8))
* **deploy:** GKE cluster, addon and Cloud SQL modules ([4b9fe55](https://github.com/juancavallotti/octo/commit/4b9fe559a78a3841d9d267db9ef92f5ff1c198a4))
* **deploy:** let the k3s VM pin Postgres to a fixed node path ([697f74f](https://github.com/juancavallotti/octo/commit/697f74f8949dd458ff04de20406681e42d562439))
* **deploy:** terraform roots for GKE and EKS, and a real data disk for the k3s database ([88d208f](https://github.com/juancavallotti/octo/commit/88d208f2829732915e1f9dd2a45d66f6ffc4d168))
* **deploy:** terraform roots for GKE Standard and Autopilot ([33284a1](https://github.com/juancavallotti/octo/commit/33284a1473f231436f014d589848148484a19316))
* **helm:** external database support, and stop leaking the password into pod specs ([ed4870f](https://github.com/juancavallotti/octo/commit/ed4870f28a7386ccf8c15d212b74d92564370b84))
* **helm:** ingress TLS modes, extra hosts, GKE CRDs and the networking seam ([219c835](https://github.com/juancavallotti/octo/commit/219c835988f44836c57cee050d77dafe99240a1d))
* **helm:** per-component image registry, digest pinning and third-party mirroring ([f1843f7](https://github.com/juancavallotti/octo/commit/f1843f7080746e62b0bbe4a7c724df729b8191b3))
* **helm:** per-target values profiles for k3d, kind, GKE and EKS ([b238423](https://github.com/juancavallotti/octo/commit/b23842302c6df0af551b9d4abef9b046ab85b297))
* **helm:** per-workload resources, security, scheduling and ServiceAccount knobs ([1dd5505](https://github.com/juancavallotti/octo/commit/1dd55054a80d2381f241a424c07a42b6c88e0ca5))
* **helm:** pin single-node database storage to a fixed host path ([d539d34](https://github.com/juancavallotti/octo/commit/d539d3492bb55e6b4a3963fd9d4ea6b83c65042f))
* **helm:** Service type, NodePorts, annotations and LB settings ([9d84c2c](https://github.com/juancavallotti/octo/commit/9d84c2c50c3a197a0b61e5788929aad70b6ae0e2))


### Bug Fixes

* **deploy:** address review findings on the terraform roots ([87233ec](https://github.com/juancavallotti/octo/commit/87233ecce411e9f0773be622530dfd65f250f35c))
* **deploy:** give EKS public nodes a public IP ([2e39ee4](https://github.com/juancavallotti/octo/commit/2e39ee45d353348f59bcc4b46ab2e6a92031af0b))
* **deploy:** make the EKS DNS records plannable ([b069484](https://github.com/juancavallotti/octo/commit/b06948482f834078ce67f9177ac1acbcafa2d342))
* **deploy:** make the GKE teardown survive its own database ([5b7d2de](https://github.com/juancavallotti/octo/commit/5b7d2de4abe55cd5fdb8bf28b40e1b31b3a3c4d8))
* **deploy:** reject an EKS hostname outside its Route53 zone ([9cf4814](https://github.com/juancavallotti/octo/commit/9cf4814639863d82e511d2b3bda67f44929b7710))
* **deploy:** shorten the EKS cluster key's deletion window ([10c3ca7](https://github.com/juancavallotti/octo/commit/10c3ca730a9ff35d2665ccce553d8a2d83659673))
* **deploy:** sweep orphaned GKE firewall rules before the VPC delete ([be9eb3d](https://github.com/juancavallotti/octo/commit/be9eb3d7741c1d7fb0d4cbeef6372a4586ec3c92))
* **helm:** guard the chart's silent failure modes ([f0fc786](https://github.com/juancavallotti/octo/commit/f0fc786d77c350619c4f50d2bde731235b0351db))
* **platform:** run the editor image as an unprivileged user ([155b724](https://github.com/juancavallotti/octo/commit/155b724d60bbb49eb0979789dfed021f1af9f714))


### Refactoring

* **deploy:** drive local k3d from the chart ([1ce9333](https://github.com/juancavallotti/octo/commit/1ce9333e69300126414cebee85b524716cafae66))
* **deploy:** make the helm-release module cloud-agnostic ([3354e48](https://github.com/juancavallotti/octo/commit/3354e48447489eedcdc205fc471939d8aaee8ed4))
* **deploy:** one tfvars per root, remote state everywhere, pinned providers ([ee8d8a6](https://github.com/juancavallotti/octo/commit/ee8d8a6b67e7b985f4f612362bd7568dbb112cd5))
* **helm:** extract the octo-common library chart ([6c83ffb](https://github.com/juancavallotti/octo/commit/6c83ffb2635c991b14b32504b895785b3718bab7))


### Documentation

* **deploy:** frame the cluster roots as a worked example ([28ac8ac](https://github.com/juancavallotti/octo/commit/28ac8acb6d53160c675268e7c8aef83f84bdd9d2))
* **deploy:** record GKE Standard as validated on a live cluster ([97ba3aa](https://github.com/juancavallotti/octo/commit/97ba3aa2a81fc63149df49f3d0e081ff9162e673))
* **deploy:** terraform provisioning for GKE and EKS ([db2da8d](https://github.com/juancavallotti/octo/commit/db2da8d68697744f3b8dd1296aacf5be3b194007))
* multi-target deployment ([7cabc90](https://github.com/juancavallotti/octo/commit/7cabc90d7a22d17186971f7c562dc588a32e8290))

## [0.6.4](https://github.com/juancavallotti/octo/compare/v0.6.3...v0.6.4) (2026-08-01)


### Features

* **editor:** run every test suite from the header ([d5f8728](https://github.com/juancavallotti/octo/commit/d5f872889b0112f455ffecbb03cb5e2accb13994))


### Bug Fixes

* **editor:** keep the header RUN control in place on the Testing tab ([531a1d1](https://github.com/juancavallotti/octo/commit/531a1d1464b5e2d5a3f82bbdbcd555e93a84c11c))
* **run-host:** scale a test run's wall clock with the suites it names ([7a33b0d](https://github.com/juancavallotti/octo/commit/7a33b0d8dfb31e62a181e8723c9f06c0ededc91c))


### Refactoring

* **editor:** drop the Try button from test expectations ([cd45fd2](https://github.com/juancavallotti/octo/commit/cd45fd2c754d13027afd8a0a2d844d84e09d755d))
* **editor:** let one test run carry many suites ([ae71614](https://github.com/juancavallotti/octo/commit/ae716146e3522ed46e608c3e1c473664712d986c))


### Documentation

* make dolphin a first-class citizen ([8811824](https://github.com/juancavallotti/octo/commit/8811824787193da836107db87417c0d8bb39696c))
* make dolphin a first-class citizen ([98a1664](https://github.com/juancavallotti/octo/commit/98a1664e284921d8371bc27d0d26a2523832445c))

## [0.6.3](https://github.com/juancavallotti/octo/compare/v0.6.2...v0.6.3) (2026-07-31)


### Features

* author and run dolphin tests from the editor ([b05703d](https://github.com/juancavallotti/octo/commit/b05703d2fd3676f746df30b4cf3910913facd5cd))
* **dolphin:** write a machine-readable run report with --report-json ([fe6ecdd](https://github.com/juancavallotti/octo/commit/fe6ecddadcc6cfdb98da96422f5211232cff942d))
* **editor:** add the test-suite store capability ([974ca75](https://github.com/juancavallotti/octo/commit/974ca75a049a04b287300e1739f04329713a561d))
* **editor:** add the Testing tab with a suite rail and YAML view ([f693322](https://github.com/juancavallotti/octo/commit/f693322b2d7c717f4554d6e34e322a70e0540c4f))
* **editor:** assert what a watched block saw ([4d322de](https://github.com/juancavallotti/octo/commit/4d322ded70f392466e46ce027b94c5d334c9aa42))
* **editor:** author a test case from a form ([11a02f1](https://github.com/juancavallotti/octo/commit/11a02f1603283c95942c3c17e5598aa135da351e))
* **editor:** edit a suite's file-level settings ([b30acd6](https://github.com/juancavallotti/octo/commit/b30acd6154a8ee566ccfe3aacfe3be17cd3392eb))
* **editor:** give each browser tab its own runner ([5b90e28](https://github.com/juancavallotti/octo/commit/5b90e286ed62e4ad2fdd0cd510e461c936aea8a2))
* **editor:** mock a block from the suite form ([461974f](https://github.com/juancavallotti/octo/commit/461974fd4f37a4e0c172d056280040a3557b5767))
* **editor:** model and serialize a dolphin test suite ([72ce0c3](https://github.com/juancavallotti/octo/commit/72ce0c385322351a54286e5019d916f574f4caed))
* **editor:** promote a run result into a test case ([68ddbf4](https://github.com/juancavallotti/octo/commit/68ddbf46e71aebb2ecd392c676c2a2c464798ced))
* **editor:** run a suite scenario from the flow ▶ menu ([6af0f83](https://github.com/juancavallotti/octo/commit/6af0f83ce205b9397469e612642797c596ee584d))
* **editor:** run tests from the Testing tab ([9f94b85](https://github.com/juancavallotti/octo/commit/9f94b85a36b3ff8eea38609a38e6765b1179afe8))
* **editor:** try a `that:` expression before running the suite ([d72b8e6](https://github.com/juancavallotti/octo/commit/d72b8e602239b01231149e26000eed23b4ac3c76))
* **mcp:** author and run dolphin test suites ([b60649b](https://github.com/juancavallotti/octo/commit/b60649b44d082e55e0536a9d6843934bb351a471))
* **mcp:** read and write the editor's flow meta ([e0fa200](https://github.com/juancavallotti/octo/commit/e0fa2007e531cfd78a87f82095d7c0f4bb0e1605))
* **mcp:** validate test input flow ([0bd9987](https://github.com/juancavallotti/octo/commit/0bd9987c2c56d48ec903b33bc52a971908718e00))
* **platform:** store dolphin suites as .octo/tests resources ([513193c](https://github.com/juancavallotti/octo/commit/513193c07f3158158543c04548c3f24f16b84557))
* run dolphin suites through the run transport ([56fffc5](https://github.com/juancavallotti/octo/commit/56fffc53a39c72fd73efaf8017c70a069ed18e2a))
* **run-host:** derive a per-tab namespace from the cookie ([b4dd81d](https://github.com/juancavallotti/octo/commit/b4dd81d7501c17473e29e534ca699f7341a0ac4d))
* **run-host:** report dolphin availability to the editor ([e285377](https://github.com/juancavallotti/octo/commit/e285377cc8349ca4af3ed817597f2172a73574c7))
* **run-host:** run dolphin suites with test() ([5f95456](https://github.com/juancavallotti/octo/commit/5f95456ce2ff4c69a5e1d39ce446eebf91ee52ed))
* **standalone:** store dolphin suites beside the flows ([edc39ec](https://github.com/juancavallotti/octo/commit/edc39ecd93cc7003569c45017de1f7b53d69d5cd))


### Bug Fixes

* **dolphin:** attempt all configured report writes ([42e415b](https://github.com/juancavallotti/octo/commit/42e415b16116606547969986352a8709ba772f9f))
* **editor:** clamp a negative crossing count to zero ([3afeb88](https://github.com/juancavallotti/octo/commit/3afeb883710a05ef2566a49f83f56a28a71a7d7a))
* **editor:** don't let a duplicated tab inherit another tab's runner ([5ccc6b5](https://github.com/juancavallotti/octo/commit/5ccc6b5cf5a9f92f9e2f9924727574a9b105dba7))
* **editor:** ignore stale suite run completions ([0bc721e](https://github.com/juancavallotti/octo/commit/0bc721e1dbc16a17f59184e1b55a2f8360c60921))
* **editor:** keep RUN working when a tab id can't be minted ([f57f463](https://github.com/juancavallotti/octo/commit/f57f46313b2fdf1421031f5114d3c2bd08ba5e69))
* **editor:** let the console's Tests tab scroll ([5e74fb2](https://github.com/juancavallotti/octo/commit/5e74fb26dff377cb3462575f7ae87cbe1bff4f05))
* **editor:** prevent suite resurrection on async races ([1be7e52](https://github.com/juancavallotti/octo/commit/1be7e52268966d41e1869e18eb6ac1b638c1f0bb))
* **editor:** quote scaffold flow yaml scalar ([85f00bc](https://github.com/juancavallotti/octo/commit/85f00bc5a09b7e9680cc3b939c9e5b9ef863d8de))
* **editor:** reload suites and flow meta when something else writes them ([9904099](https://github.com/juancavallotti/octo/commit/99040998c0a8bad909a9b0370710b22c60c43453))
* **editor:** render the filtered view-mode options ([785cd7d](https://github.com/juancavallotti/octo/commit/785cd7db48d4b9d58d12a421ed4eea64a25b58f8))
* **editor:** trim case names in duplicate validation ([d33d9cc](https://github.com/juancavallotti/octo/commit/d33d9cc930feaeb3898ab03e963601ea1731fd90))
* **mcp:** re-key flow meta when update_flow renames a flow ([25f6b47](https://github.com/juancavallotti/octo/commit/25f6b47f44f040f2c07436f8310969f7d71883f3))
* **mcp:** report non-object stored vars as text ([ebee4eb](https://github.com/juancavallotti/octo/commit/ebee4eb23238effc8b28b580897f03f50f7fc460))
* **platform:** only claim template resources as suites ([7c18987](https://github.com/juancavallotti/octo/commit/7c18987b5f4ae2666acae4b752a750b9cfdadb9e))
* **run-host:** avoid suite filename collisions ([94defe3](https://github.com/juancavallotti/octo/commit/94defe31b498e79ddcd3e8dbded18b3fb939cf43))
* **run-host:** give each editor run its own admin port ([fab7506](https://github.com/juancavallotti/octo/commit/fab7506563e50da6be75ebbaffb543065fd0d83a))
* **run-host:** roll a start back when a port pool is exhausted ([0f124ae](https://github.com/juancavallotti/octo/commit/0f124ae5c8a338391a2d617ae573d2f4a47e00df))
* **runtime:** survive an admin port that is already taken ([4ff8131](https://github.com/juancavallotti/octo/commit/4ff8131a887d484e1b9b51022385aaf9d4986d17))
* **standalone:** keep *_test.yaml out of the flow list ([3256b44](https://github.com/juancavallotti/octo/commit/3256b44b2b7033d10858f3ca46df26ada00fd9c7))
* stop the monitoring port from blocking a second run ([7957267](https://github.com/juancavallotti/octo/commit/7957267299fc2b1f566e53834a8536c0044a4696))


### Refactoring

* **connectors:** one init per connector module ([41fe232](https://github.com/juancavallotti/octo/commit/41fe232d9ec489f9e52b2583f0d38997ea51b7f3)), closes [#215](https://github.com/juancavallotti/octo/issues/215)
* **core:** move ai-mapping and ai-embed into the engine ([631915a](https://github.com/juancavallotti/octo/commit/631915a30b99792dc8ad5e0a68f57693a6a4f857))
* **core:** one init per module in expr and engine ([1c82ce4](https://github.com/juancavallotti/octo/commit/1c82ce44ca5507fa4fe46ea2aefec3e669b65804)), closes [#215](https://github.com/juancavallotti/octo/issues/215)
* **editor:** expose the meta and suite models on the runtime subpath ([bb7870a](https://github.com/juancavallotti/octo/commit/bb7870a4ef5ec0992c24c468138dbf7b4ba2ff84))
* **editor:** share the dolphin suite naming rule between hosts ([41c0788](https://github.com/juancavallotti/octo/commit/41c07880165644fb8a73296724ea1251b748ae16))
* one init() per loadable module, enforced ([72bbb03](https://github.com/juancavallotti/octo/commit/72bbb03641b700dbf00647cb6116bffbeb06fa75))


### Documentation

* correct the JSON report's shape and the suite-naming advice ([8fd20cd](https://github.com/juancavallotti/octo/commit/8fd20cd0f9f7232305ff010109b1cc5aa6ab3ec3))
* note the one non-manifest init() the allowlist permits ([4f08c0d](https://github.com/juancavallotti/octo/commit/4f08c0df95b007292794b8b1383e649637cba3ac))
* the Testing tab, dolphin's JSON report, and the new MCP tools ([77cc09e](https://github.com/juancavallotti/octo/commit/77cc09e5cc96848ca113aca09dd1e0a4f0957b39))
* write down the one-init-per-module rule ([3b62b1f](https://github.com/juancavallotti/octo/commit/3b62b1fa99ce046e5823ae0aa0703cc95b77703b)), closes [#215](https://github.com/juancavallotti/octo/issues/215)

## [0.6.2](https://github.com/juancavallotti/octo/compare/v0.6.1...v0.6.2) (2026-07-29)


### Features

* **ai:** add ai-embed block ([abf104c](https://github.com/juancavallotti/octo/commit/abf104cab154a1f4289cffde710975ec1b14a6b9))
* **llm/gemini:** implement Embed ([1c8da59](https://github.com/juancavallotti/octo/commit/1c8da591f53846675afd5194b78739c55f26d3bc))
* **llm/openai:** implement Embed ([437d6af](https://github.com/juancavallotti/octo/commit/437d6af0bf46b6746fdf35f8ab6e9f22d425bd15))
* **llm:** add EmbedClient interface and DTOs ([26c3d2f](https://github.com/juancavallotti/octo/commit/26c3d2ff029ef3370f3729375e22bffce16b8878))
* **pinecone:** add connector with eager dimension validation ([99f5f20](https://github.com/juancavallotti/octo/commit/99f5f201c1f6562d2248633184efc2747e393082))
* **pinecone:** add delete block ([a5a3e99](https://github.com/juancavallotti/octo/commit/a5a3e99d84d8c1b77af591ea3a536b914217a15e))
* **pinecone:** add fetch block ([262f8b3](https://github.com/juancavallotti/octo/commit/262f8b381f2e03a35c340e3a5d681761bb46bcf0))
* **pinecone:** add query block ([7dcc28a](https://github.com/juancavallotti/octo/commit/7dcc28a9ccad6b1bc1573e34363f3d4a1120a9e2))
* **pinecone:** add upsert block ([ab008d1](https://github.com/juancavallotti/octo/commit/ab008d1c7d1089ed33cb1f66a717678f086183d3))
* **pinecone:** results to the body, a direct index host, and review fixes ([c3e6cc6](https://github.com/juancavallotti/octo/commit/c3e6cc63a4d2c794f6d1bb7ab4c3c9bc2c7ad38e))
* **runtime:** register pinecone package ([a6f3d5c](https://github.com/juancavallotti/octo/commit/a6f3d5c13062ad51d83be796d1c43b8a5fe1f8cd))
* **samples:** add a RAG pipeline sample with an answering agent ([e7b4098](https://github.com/juancavallotti/octo/commit/e7b4098c8b9b019744f1bef106f8b414c4e79f32))


### Bug Fixes

* **ai:** give ai-embed its own icon ([cbb2eb6](https://github.com/juancavallotti/octo/commit/cbb2eb67a047294d9800f220fe0a700639adcb10))
* **deploy:** publish Cloud Build's private images under the -paas names ([88454c3](https://github.com/juancavallotti/octo/commit/88454c32584876cbab20fa4b6284d487a8a1dc36))
* **deploy:** publish Cloud Build's private images under the -paas names ([d6e28ec](https://github.com/juancavallotti/octo/commit/d6e28ec135f1dea25c45a94ff53018830a0efc35))
* **mcp:** advertise Octo's icon in the initialize handshake ([a0d82ed](https://github.com/juancavallotti/octo/commit/a0d82edfcfcdec4c40edfd177ef52922858125c2))
* **mcp:** advertise Octo's icon in the initialize handshake ([f6ce709](https://github.com/juancavallotti/octo/commit/f6ce70982b7e5db9695654c9a76c40695d85298f))
* **mcp:** allow partial serverInfo overrides in handler options ([111bd2d](https://github.com/juancavallotti/octo/commit/111bd2dcd6bea4086f32bdc23eb0946b666327a3))
* **pinecone:** give it its own palette group and brand icon ([b2d68e7](https://github.com/juancavallotti/octo/commit/b2d68e7a9228395a16e97efce1e8ae7805c7f0f8))


### Documentation

* **ai:** distinguish connector instances from connector types ([3d0a934](https://github.com/juancavallotti/octo/commit/3d0a934c1e7946984f9288620cb02009f77a2fd6))
* **ai:** document ai-embed ([69cc4fc](https://github.com/juancavallotti/octo/commit/69cc4fc3cd3dfe92e11d2fc9be6e97a2d71fa382))
* **guides:** document the RAG pipeline ([e51845b](https://github.com/juancavallotti/octo/commit/e51845b457a40ebf2ba4838ced64b26353a3d956))
* **pinecone:** add connector reference page ([faa0a05](https://github.com/juancavallotti/octo/commit/faa0a0504f9053f6af221370c0661f84d5abdbc3))

## [0.6.1](https://github.com/juancavallotti/octo/compare/v0.6.0...v0.6.1) (2026-07-29)


### Features

* **deploy:** support a separate domain for per-integration subdomains ([d820c33](https://github.com/juancavallotti/octo/commit/d820c3351c5dad85476ad6e181ede9a5114e1ff0))
* **deploy:** support a separate domain for per-integration subdomains ([fae630d](https://github.com/juancavallotti/octo/commit/fae630db447b460daa73e87c728eacf27511e88a))
* **release:** publish the PaaS images publicly on Docker Hub ([7a0ca28](https://github.com/juancavallotti/octo/commit/7a0ca281d8d03b4e97ad6946e1433ceb7100abe5))
* **release:** publish the PaaS images publicly on Docker Hub ([7640bff](https://github.com/juancavallotti/octo/commit/7640bff79aa29a3c809784cfdafec3cc76d0823c))
* **runtime:** derive default flow workers from GOMAXPROCS ([4f57a61](https://github.com/juancavallotti/octo/commit/4f57a6163bed2a53020e3d2e368eb12c5b7e3cb2)), closes [#197](https://github.com/juancavallotti/octo/issues/197)


### Bug Fixes

* address CodeRabbit review on the runtime quick-wins PR ([0cc5ab3](https://github.com/juancavallotti/octo/commit/0cc5ab34a782042ce1833d37e380fb83aae88cf6))
* **build:** restore repo-root context for the runtime image build ([ac255b9](https://github.com/juancavallotti/octo/commit/ac255b90b66ea484b42e208d50fd582080d8066c))
* **database:** default the connection pool instead of falling through to Go's built-in of 2 ([0760392](https://github.com/juancavallotti/octo/commit/076039298db37b34e59e92ee7fdef7aac636daff)), closes [#190](https://github.com/juancavallotti/octo/issues/190)
* **deploy:** correct wildcard-cert docs (CodeRabbit review) ([a564c2b](https://github.com/juancavallotti/octo/commit/a564c2be670d8767bc26be2af38dbfa65cbabba4))
* **deploy:** plumb apps_domain through Cloud Build's automatic deploy ([6c6edc3](https://github.com/juancavallotti/octo/commit/6c6edc3fbb97b659571defef7e3b00dec7b24367))
* **orchestrator:** make per-integration Ingress class/annotations/TLS configurable ([6f949ce](https://github.com/juancavallotti/octo/commit/6f949ceb8f69c4a4d936de8858df62b242d9bc18))
* **orchestrator:** make per-integration Ingress class/annotations/TLS configurable ([c0415f1](https://github.com/juancavallotti/octo/commit/c0415f1d7959e268420a5313797527ca26c57044)), closes [#201](https://github.com/juancavallotti/octo/issues/201)


### Performance

* **core:** switch the event bus to copy-on-write ([1e6e213](https://github.com/juancavallotti/octo/commit/1e6e21339dd31aad244a4a923514e177148475c8)), closes [#205](https://github.com/juancavallotti/octo/issues/205)

## [0.6.0](https://github.com/juancavallotti/octo/compare/v0.5.0...v0.6.0) (2026-07-26)


### ⚠ BREAKING CHANGES

* **observability:** core.BlockEvents drops AddAsync, AsyncBlockListener, Dropped and Stop. The octo_block_events_dropped_total metric is removed.

### Features

* **editor:** label a composite's slot when it is not the block's body ([af64ff5](https://github.com/juancavallotti/octo/commit/af64ff5b450ee2543c2aeb73c13395352d5ef44b))
* **engine:** let a block take over a flow and continue it on borrowed workers ([27ed139](https://github.com/juancavallotti/octo/commit/27ed1390d59f6ec325c21f02c53f099de70bc976))
* **engine:** split and aggregate blocks ([#178](https://github.com/juancavallotti/octo/issues/178), [#179](https://github.com/juancavallotti/octo/issues/179)) ([5f3e001](https://github.com/juancavallotti/octo/commit/5f3e0014a8cf9f8a14563ad3b28cbe38b25249fc))


### Bug Fixes

* **core:** contain a panicking block listener instead of propagating it ([912e011](https://github.com/juancavallotti/octo/commit/912e011ad2d2c460ca1d46337e6973eaaa5e91a4))
* **engine:** deliver a completed group outside the fold stripe ([85f5fbc](https://github.com/juancavallotti/octo/commit/85f5fbc026e6022e891ac604690d46d647efa3fb))
* **engine:** make the aggregate's group index a real ledger ([5c87418](https://github.com/juancavallotti/octo/commit/5c87418b955477f4a63f55ca3204d2fd800c770e))
* **observability:** deliver block events inline, not through a queue ([c314462](https://github.com/juancavallotti/octo/commit/c31446221857bbff9db5c0376167147bc0931bc7))
* **runtime:** finish the shutdown even when the source fails to stop ([b1a2b69](https://github.com/juancavallotti/octo/commit/b1a2b69620e80e43f99c8e97ff4f0504dd4de5bf))


### Performance

* **observability:** cache the collectors a watched block resolves to ([374c0a3](https://github.com/juancavallotti/octo/commit/374c0a3d551a888b027a7b195dbd437f7fb9bd4d))


### Documentation

* block events are inline and filtered at the source ([c3c1071](https://github.com/juancavallotti/octo/commit/c3c107182be216f9a4c8f1c7ee38686ca9e12f9c))

## [0.5.0](https://github.com/juancavallotti/octo/compare/v0.4.3...v0.5.0) (2026-07-26)


### ⚠ BREAKING CHANGES

* **config:** ${NAME} substitution now applies to every value in a config document, not only to `settings` values. A reference in a position that was previously left alone — a block `condition`, a flow `name`, an `ai-agent` `prompt` — is now resolved, and one naming a variable that is not declared under `env:` fails the load instead of surviving as literal text. The `env:` and `resources:` sections are read to build the environment before substitution runs, so they stay literal. The undeclared-reference error is no longer worded as being about settings.

### Features

* **core:** add the block event dispatcher ([cd56ff8](https://github.com/juancavallotti/octo/commit/cd56ff86a151c94e93b205bf2563d533b4023b44))
* **editor,mcp:** catalogue the CEL extension libraries ([f4afd55](https://github.com/juancavallotti/octo/commit/f4afd55a98110fef24d203b537153582a61898b3))
* **engine:** carry a flow's duration and failing block on its terminal event ([50799b6](https://github.com/juancavallotti/octo/commit/50799b6b21c6939b3e7ffa5b90cf63fb0ac25c57))
* **engine:** emit pre/post-invoke around every block ([6ce3237](https://github.com/juancavallotti/octo/commit/6ce323707639166815ff9c41e6112bfdc6824079))
* **engine:** mint each block's flow path at build time ([068e2ab](https://github.com/juancavallotti/octo/commit/068e2ab0b4bffa9a58feae4d64088f3c5542c372))
* **expr:** enable cel-go's strings, lists, encoders, math, comprehensions, sets, and regex libraries ([fb6360f](https://github.com/juancavallotti/octo/commit/fb6360f442e093b26ba6bb29acabacac1ebc11bd))
* **expr:** enable the cel-go utility extension libraries ([d636df1](https://github.com/juancavallotti/octo/commit/d636df13da2fee8d87a325104a31e1dc2df10f79)), closes [#174](https://github.com/juancavallotti/octo/issues/174)
* native health probes and Prometheus metrics ([5ebfc4d](https://github.com/juancavallotti/octo/commit/5ebfc4db4e1edeea3e2885f80a5bdb23fdd01766))
* **observability:** expose per-block metrics for addressed blocks ([1fc0580](https://github.com/juancavallotti/octo/commit/1fc05809395bf8328b33ca4362c877961745e0c4))
* **observability:** expose per-flow prometheus metrics ([9902fca](https://github.com/juancavallotti/octo/commit/9902fca2f7c15d38ea13a977c5a6e5dc2fb068b1))
* **observability:** serve liveness and readiness probes on an admin port ([934e1aa](https://github.com/juancavallotti/octo/commit/934e1aa2c68539ab7a68ae219956eef09977ebc8))
* **orchestrator:** give runtime deployments liveness and readiness probes ([720ebea](https://github.com/juancavallotti/octo/commit/720ebeaa59fa9423eb93aaa138244d762cadf357))
* **runtime:** pre/post-invoke block events, addressed by flow path ([cbcd4c8](https://github.com/juancavallotti/octo/commit/cbcd4c8fb83690667dffcdae5c01afcb45b6116f))
* **runtime:** wire block events into the service ([162a046](https://github.com/juancavallotti/octo/commit/162a046584f8dd8e224efc0ad26d087871cf9771))
* **samples:** add cel-extensions, a no-foreach normalization flow ([30e8a18](https://github.com/juancavallotti/octo/commit/30e8a18fd063432eb36fb9a3b92d1e52921d38a7))
* **services:** add a hosted-service facet for process-lifetime runtime services ([366070b](https://github.com/juancavallotti/octo/commit/366070bad408c721859a42de31aa8a33b05b1c89))
* **types:** add Message.Scoped and MutableBody for copy-on-write bodies ([a04b32b](https://github.com/juancavallotti/octo/commit/a04b32b94e857ec572f3a06e5bac456c04421e00))
* **types:** add the block invoke event payload ([c614981](https://github.com/juancavallotti/octo/commit/c614981ade10f954215f45c2fd473bd524fbdb79))


### Bug Fixes

* address CodeRabbit review on the observability PR ([4907d51](https://github.com/juancavallotti/octo/commit/4907d51bd7485c0a9de6778dfc4171bce3836c41))
* **config:** substitute ${NAME} before decode, in every value ([8dcc4db](https://github.com/juancavallotti/octo/commit/8dcc4dbfe597ec79807a30247e63cde2cb928044)), closes [#168](https://github.com/juancavallotti/octo/issues/168)
* **editor,mcp:** resolve namespaces by own property, and list every optional and bit function ([8869dbe](https://github.com/juancavallotti/octo/commit/8869dbe170fc844adcfe569a71a1aed90536fab4))
* **engine:** measure a block's duration without its pre-invoke listeners ([b9a2bc1](https://github.com/juancavallotti/octo/commit/b9a2bc1400306179217a0c071418a1fbdb7365d0))
* **expr:** give template {{ }} spans the registered CEL functions ([d4d9140](https://github.com/juancavallotti/octo/commit/d4d9140fb46e2e1106b63c539a1ef48a938a9f1f)), closes [#170](https://github.com/juancavallotti/octo/issues/170)
* five runtime bugs found by the performance lab ([#167](https://github.com/juancavallotti/octo/issues/167)–[#171](https://github.com/juancavallotti/octo/issues/171)) ([da926d8](https://github.com/juancavallotti/octo/commit/da926d80184d07fbdc1125fdc5cc5e2affc129f3))
* **httpclient:** pool outbound connections instead of dialling per request ([e8ceb4f](https://github.com/juancavallotti/octo/commit/e8ceb4f1f38a2d7b8a198454f62848e95d0c4ca0)), closes [#167](https://github.com/juancavallotti/octo/issues/167)
* **http:** exit zero on a clean SIGTERM shutdown ([9c8d7f7](https://github.com/juancavallotti/octo/commit/9c8d7f7726271d29ed71a0d39527d0f93b917a4f)), closes [#171](https://github.com/juancavallotti/octo/issues/171)
* **http:** hold Stop open until the accept loop releases the port ([d8cefd8](https://github.com/juancavallotti/octo/commit/d8cefd8a9c12eecde53d6a1a1593dedde9e45a72))
* **mcp:** file reverse under both the strings and lists libraries ([7c19a50](https://github.com/juancavallotti/octo/commit/7c19a506b3cf89f653a3f84ff76d1b3669973262))
* **observability:** bind before metric subscriptions ([82ec92a](https://github.com/juancavallotti/octo/commit/82ec92a3b6f24314617f13076319f48fd3d38847))
* **resources:** decide resource containment with filepath.Rel ([2d6185c](https://github.com/juancavallotti/octo/commit/2d6185c14daa59a3e63683e53946ffc403caa436))
* **resources:** refuse a resource id that leaves the root by symlink ([d2e6f26](https://github.com/juancavallotti/octo/commit/d2e6f26ab6365dcf73fc54e8bb833fb4b7889d54))
* **resources:** resolve template resources under a relative --config ([9f801d2](https://github.com/juancavallotti/octo/commit/9f801d2523e74e85daf1bd8c3f6c5e2ff044a973)), closes [#169](https://github.com/juancavallotti/octo/issues/169)
* **samples,docs:** carry money in integer cents instead of rounding doubles ([8159d97](https://github.com/juancavallotti/octo/commit/8159d97de159230191f3ceedd8c5099f7a37b43f))
* **types:** bound the body walk so a cycle cannot overflow the stack ([27af9a1](https://github.com/juancavallotti/octo/commit/27af9a163730dc55ee7c8df27ee28d14d14c956d))
* **types:** track residual body aliasing apart from pending copy-on-write ([bdaddc9](https://github.com/juancavallotti/octo/commit/bdaddc972ef8fde2aadd4d050a530e171e37c765))


### Performance

* **engine:** map foreach iterations over a shared body ([aebe38b](https://github.com/juancavallotti/octo/commit/aebe38b8f32c852cd2ecebffa748c2255910167a)), closes [#166](https://github.com/juancavallotti/octo/issues/166)
* **engine:** run enrich scopes over a shared body ([bffb271](https://github.com/juancavallotti/octo/commit/bffb27169fcc085a28266eed090d5e69e58bfe16))
* make foreach map mode linear in the collection size ([dd065f8](https://github.com/juancavallotti/octo/commit/dd065f8a0f602c5a847e5f28cb67fbf155200be6))
* **types:** deep-copy message bodies structurally instead of through JSON ([32cc61e](https://github.com/juancavallotti/octo/commit/32cc61e1047060ae655d310dc10b80516d7fa6b3))


### Refactoring

* **core:** extract the block-path grammar into core ([4027f90](https://github.com/juancavallotti/octo/commit/4027f900b8317e18814ce1149bf9265e5dc93eff))
* **types:** drop the write-only body aliasing flag ([bf58325](https://github.com/juancavallotti/octo/commit/bf58325b2067124fc6fab352ce3e5f2e7ded1ea8))
* **types:** extract the body deep-copy out of Message.Clone ([4f67aa7](https://github.com/juancavallotti/octo/commit/4f67aa7db1771a22bf8c2435e134b26044430aaf))


### Documentation

* add the extending section ([ad97578](https://github.com/juancavallotti/octo/commit/ad97578c220258065c300f98d4b747963b894578))
* align observability and extension docs ([bc2e734](https://github.com/juancavallotti/octo/commit/bc2e7346986fa1a136e4a8744c2bde2694a066d9))
* **cel:** add the extension libraries reference ([25501c1](https://github.com/juancavallotti/octo/commit/25501c1cfddc4fbe042325945e48f6b00e515410))
* declare FLOW_WORKERS in the substitution example ([a013212](https://github.com/juancavallotti/octo/commit/a013212e2e3a0d5d1b340c73191e7ae6b1122a92))
* describe message scoping alongside cloning ([f45c1ba](https://github.com/juancavallotti/octo/commit/f45c1bab834302a46b2ccde2d101492af84d7eea))
* document block invoke events ([533f629](https://github.com/juancavallotti/octo/commit/533f629d72d112bdfdad84e0c0988a79cfbc69d7))
* document the admin port, probes, and metrics ([de865bf](https://github.com/juancavallotti/octo/commit/de865bf7a109bcf75a471901dc0a5bc6bf49e232))
* finish the clone-to-scope pass over the block references ([0cc993b](https://github.com/juancavallotti/octo/commit/0cc993befbe1dfdc76c42d3d19d2d3d4a6faccc7))
* **http:** bound what the pool section promises about reuse ([2da7842](https://github.com/juancavallotti/octo/commit/2da7842720171bdd34d85792624ab5d976e655ef))
* sharpen the wording on body scoping and cycle handling ([6993790](https://github.com/juancavallotti/octo/commit/6993790247657d3a96a3b9c617dce803016e5544))
* state the runtime extension points ([9f3c2eb](https://github.com/juancavallotti/octo/commit/9f3c2eb5264b4c11fa468923bb448ee770077e53))

## [0.4.3](https://github.com/juancavallotti/octo/compare/v0.4.2...v0.4.3) (2026-07-19)


### Features

* **cli:** octo invoke --content-type for raw (non-JSON) bodies ([0c97d1a](https://github.com/juancavallotti/octo/commit/0c97d1ad8c459d44ff692b377c4c23be91dbc6e0)), closes [#142](https://github.com/juancavallotti/octo/issues/142)
* **editor:** default a cron flow's run to its source payload ([4729cb3](https://github.com/juancavallotti/octo/commit/4729cb3c04fd762353897011c0c9b266674aba2d))
* **editor:** offer a cron source's payload as a ready-made test input ([37f0700](https://github.com/juancavallotti/octo/commit/37f0700ee4e101cdd6fa384a868c5a161d806b60)), closes [#143](https://github.com/juancavallotti/octo/issues/143)


### Bug Fixes

* **dolphin:** --env-file must beat an exported shell variable ([7c88663](https://github.com/juancavallotti/octo/commit/7c886636c4a1a5a85e9ede5e1de20f45cabac671)), closes [#162](https://github.com/juancavallotti/octo/issues/162)
* **editor:** give spy records unique keys across runs ([392acb5](https://github.com/juancavallotti/octo/commit/392acb5f2185853beb4b52c29fbf5183865948e8))

## [0.4.2](https://github.com/juancavallotti/octo/compare/v0.4.1...v0.4.2) (2026-07-14)


### Features

* **cli:** make the octo CLI go-installable ([fde2e4a](https://github.com/juancavallotti/octo/commit/fde2e4ac67402240fb2ff1e0c149d4803346a41a))
* **cli:** make the octo CLI go-installable ([e807f8d](https://github.com/juancavallotti/octo/commit/e807f8d22b2b160038f9f69ed2e4510a7b45736a))
* **cli:** report a uniform outcome envelope from octo invoke ([20cdbce](https://github.com/juancavallotti/octo/commit/20cdbce83c7d91d389bb23ad97e56119950a9651))
* **cli:** write the invoke envelope to a file ([9a72d44](https://github.com/juancavallotti/octo/commit/9a72d44e569428302bb529352e967437ad456a56))
* **dolphin:** a test runner for octo flows ([3fbded2](https://github.com/juancavallotti/octo/commit/3fbded2ae563301c4ef22e7188300d09025aa823))
* **dolphin:** assert on the result and on what the spies saw ([3213f49](https://github.com/juancavallotti/octo/commit/3213f49775bbfdc7afb5b52f64da1e4476956be3))
* **dolphin:** bootstrap the dolphin CLI ([3b299f4](https://github.com/juancavallotti/octo/commit/3b299f45a3f7e091e7f5a55e3964bb4665b41688))
* **dolphin:** console and JUnit XML reports ([cdb138d](https://github.com/juancavallotti/octo/commit/cdb138df28aa3451232ff0750ce14cab74f112e1))
* **dolphin:** discover suites and pair them with their flows ([6076ccd](https://github.com/juancavallotti/octo/commit/6076ccd772dc4d97375c1c178aebb3e7f668c521))
* **dolphin:** give a suite its own environment ([dca08f1](https://github.com/juancavallotti/octo/commit/dca08f15dc91529c7a2f32e368f4e4e0b6495c2c))
* **dolphin:** print the test-file schema ([0b9ecf7](https://github.com/juancavallotti/octo/commit/0b9ecf7bec0b46fb592d2d6e67aa3b140677ef41))
* **dolphin:** read octo's outcome from a file, not stdout ([0088498](https://github.com/juancavallotti/octo/commit/0088498bd4bffe01ddfd910a2ab20b1f9069a65a))
* **dolphin:** run the cases against octo ([530dad3](https://github.com/juancavallotti/octo/commit/530dad3d2e48d496dc7a99fb9004473341a5eda3))
* **dolphin:** the test file format ([d56d6a7](https://github.com/juancavallotti/octo/commit/d56d6a707da4bf0b4c4a62d2a46bbcb013642254))


### Bug Fixes

* **dolphin:** say what a failed assertion was judged against ([36dd26e](https://github.com/juancavallotti/octo/commit/36dd26e02db3636abc750a549f321d9e0b1cf0c6))
* **runtime:** keep a non-scalar env value a string ([8eda3a4](https://github.com/juancavallotti/octo/commit/8eda3a49861cf5c3388c956af06d956d67b6b8f2))
* **runtime:** skip *_test.yaml when loading a config directory ([ff09d80](https://github.com/juancavallotti/octo/commit/ff09d8027b2d7bead4e5105e1f603ad9ddc3c0d2))


### Refactoring

* **cli:** share the schema formatter between the CLIs ([f498a52](https://github.com/juancavallotti/octo/commit/f498a5272fc05ecd8a5a5f7c46adb8dda74c2afc))


### Documentation

* **deploy:** ship integrations as self-contained runtime images ([7fb94e3](https://github.com/juancavallotti/octo/commit/7fb94e33a67bc54c1c8b99aa69920d91ebb129d9))
* document `go install` and drop the Go workspace framing ([1ecfefc](https://github.com/juancavallotti/octo/commit/1ecfefc2820d4699fa65c444b2b68f162165c685))
* document the dolphin test runner ([f7e40d2](https://github.com/juancavallotti/octo/commit/f7e40d2b6e2c81f49c1602ee051b7eb6c10656aa))
* give every card an icon ([37afa46](https://github.com/juancavallotti/octo/commit/37afa461687afc1c2ffe295c57034097907ab55d))
* lead the home page with why Octo exists ([3a767b1](https://github.com/juancavallotti/octo/commit/3a767b10b5de52060aef62d74a16050f443ae33a))
* self-hosted deploy guide, card icons, and a home page that says why ([8d79bd3](https://github.com/juancavallotti/octo/commit/8d79bd31f9340ad2361b69101c231636b9258805))
* testing flows with dolphin ([57a5252](https://github.com/juancavallotti/octo/commit/57a52525dfe27609fe31c33ec77cfa6d6901b0d6))

## [0.4.1](https://github.com/juancavallotti/octo/compare/v0.4.0...v0.4.1) (2026-07-12)


### Features

* **database:** accept the password as a separate setting ([5c24d2d](https://github.com/juancavallotti/octo/commit/5c24d2d85d147ced62a890d958b2e8f11b1f40a4)), closes [#119](https://github.com/juancavallotti/octo/issues/119)
* **foreach:** add a map mode that collects each iteration's body ([be9d983](https://github.com/juancavallotti/octo/commit/be9d98386256ac347ab0b9e3d13aec4568ec9464)), closes [#129](https://github.com/juancavallotti/octo/issues/129)
* **notion:** capture the verification token in memory ([bf15b89](https://github.com/juancavallotti/octo/commit/bf15b89d15aafdeefd4faca28cf6a3465ef8b59b)), closes [#154](https://github.com/juancavallotti/octo/issues/154)
* **schema:** publish each composite's addressable branches ([7888655](https://github.com/juancavallotti/octo/commit/788865582617c139a8a9dae4e9910acd65146aa7)), closes [#153](https://github.com/juancavallotti/octo/issues/153)


### Bug Fixes

* **expr:** source payloads see the registered CEL extensions ([158c9dd](https://github.com/juancavallotti/octo/commit/158c9dd661bf7faf455893c113563141d24db34d)), closes [#132](https://github.com/juancavallotti/octo/issues/132)
* **notion:** bootstrap a webhook subscription without a token ([9a516cd](https://github.com/juancavallotti/octo/commit/9a516cd4ddd7ebdeeac5791c4833124a92a2a18a)), closes [#154](https://github.com/juancavallotti/octo/issues/154)


### Refactoring

* **core:** thread the resource loader into source construction ([9243fe5](https://github.com/juancavallotti/octo/commit/9243fe5c5a9917e7c062626bda820903ad8c4df9))


### Documentation

* **mcp:** point invoke_flow at getSchema for branch names ([33db20e](https://github.com/juancavallotti/octo/commit/33db20eafb0f28c1577e6bd84e2f0376bc6d55e2)), closes [#153](https://github.com/juancavallotti/octo/issues/153)

## [0.4.0](https://github.com/juancavallotti/octo/compare/v0.3.8...v0.4.0) (2026-07-11)


### ⚠ BREAKING CHANGES

* **cli:** `octo invoke` prints the result message ({event_id, variables, body}) instead of the bare result body. A caller that consumed stdout as the body — piping into `jq '.field'`, or parsing the MCP invoke_flow `output` — reads `.body` of the envelope instead.

### Features

* **cli:** --run-debug-config, a debugging run in one file ([975775d](https://github.com/juancavallotti/octo/commit/975775d844193062489017e37fe2de0f4d649316))
* **cli:** --spies and --mocks, and the debug output envelope ([5c18749](https://github.com/juancavallotti/octo/commit/5c187499d9fb1721fd47dea4f80a43d8bf83347b))
* **cli:** invoke prints the whole result message ([ada7a5d](https://github.com/juancavallotti/octo/commit/ada7a5d1fe2718e9793eebf4a66e2209b4c70022))
* **cli:** print the debug-config schema ([2bc57eb](https://github.com/juancavallotti/octo/commit/2bc57ebfc160656dce8561a4abdcb11f284101ba))
* **core:** mock spec and the engine's mock block ([aec395e](https://github.com/juancavallotti/octo/commit/aec395ed22aa453e19be68a874d400b7f85eb930))
* **core:** spy collector and the engine's spy block ([a16482e](https://github.com/juancavallotti/octo/commit/a16482e41d518b911104a03965e183d1aa2afbdc))
* **editor:** derive durable block addresses for mocks and spies ([0fdce98](https://github.com/juancavallotti/octo/commit/0fdce98864da5de7b868a845f37dc910620d1006))
* **editor:** mock and watch a block from the canvas ([919fd86](https://github.com/juancavallotti/octo/commit/919fd868fb2299b900e35ec80ad485118598b1e6))
* **editor:** model mocks and spies in the editor meta file ([ef53e80](https://github.com/juancavallotti/octo/commit/ef53e803fc7ef24be8a7ae1fea35ba95d6d5e714))
* **editor:** run flows under the mocks and spies the canvas holds ([9c2437a](https://github.com/juancavallotti/octo/commit/9c2437ad56d881dfeca4c99b5e4e167576c018f8))
* **mcp:** edit one flow without rewriting the whole integration ([97eb552](https://github.com/juancavallotti/octo/commit/97eb552afe7011cb31a7f58c58d448ddf9ef60bf))
* **mcp:** invoke_flow can place spies and mocks ([17df8bf](https://github.com/juancavallotti/octo/commit/17df8bf5ee8fd1df48d0033ff18f4713999e1d87))
* mocks and spies in the editor and MCP, and flow-level CRUD ([64922a3](https://github.com/juancavallotti/octo/commit/64922a3c881ef35259c67879e5cacd5b2751fd05))
* **run-host:** carry spies and mocks through invoke ([2562e68](https://github.com/juancavallotti/octo/commit/2562e688531e2acf2888db2901fd87326e6a2503))
* **runtime:** inject mocks in place of addressed blocks ([dd178b9](https://github.com/juancavallotti/octo/commit/dd178b99dbd2a2f84e4a20a1b652b0a07e09efeb))
* **runtime:** inject spies at addressed blocks ([6d5f442](https://github.com/juancavallotti/octo/commit/6d5f4425cfaf22f20a847e98af82ac023b5eeb56))
* spies and mocks for octo invoke ([c226c79](https://github.com/juancavallotti/octo/commit/c226c79a563ab21e1dba984c833fcca680da1f98))


### Bug Fixes

* **mcp:** a deleted flow takes its doc comment with it ([da50c4f](https://github.com/juancavallotti/octo/commit/da50c4fe3c9506b7f50e9cd8c1080f63f21fbad8))
* **mcp:** validate against the runtime's catalogue, not an empty fallback ([b0c35c3](https://github.com/juancavallotti/octo/commit/b0c35c3f42da88bcb8f5f1871efbfd41235da8ab))


### Refactoring

* **cli:** split main.go into one file per command ([56c66fb](https://github.com/juancavallotti/octo/commit/56c66fb3edc671dff9d3ca33bf705619795546d1))
* **runtime:** extract the shared block-address resolver ([3f02699](https://github.com/juancavallotti/octo/commit/3f0269960f70c4ce6cf6b80fbdb5ee9219bc2f75))


### Documentation

* mocks and spies in the editor and MCP ([f355be0](https://github.com/juancavallotti/octo/commit/f355be0d1c52407e186b164c7c03b6dbdd19a410))
* shoot the mock form, the spy badge and the spy records ([4a058e7](https://github.com/juancavallotti/octo/commit/4a058e7295cdd3cc61fddeeea9c4a0ea34992433))
* spies, mocks, and the debug seam ([02eed50](https://github.com/juancavallotti/octo/commit/02eed5001e7b5ce89bc306752ae345ede2d9e69b))
* stop for approval before each commit ([196b87a](https://github.com/juancavallotti/octo/commit/196b87a6a251f5c8305721346acb74c720981b8c))

## [0.3.8](https://github.com/juancavallotti/octo/compare/v0.3.7...v0.3.8) (2026-07-11)


### Features

* **ci:** publish CLI binaries and a runtime-only image on release ([fdeac0c](https://github.com/juancavallotti/octo/commit/fdeac0ced088aa3eb80d37fa70a50af08002a291))
* **ci:** release the CLI for macOS/Linux/Windows and a runtime-only Docker image ([67d537a](https://github.com/juancavallotti/octo/commit/67d537ab6ad23a2fcb2b450b82c2425cd754d8d7))
* **cli:** add invoke --break-at with a JSON envelope ([04ae5fc](https://github.com/juancavallotti/octo/commit/04ae5fc0efb48f9fad3e1f17407d845fef633ee6))
* **core:** add the Breakpoint collector ([65ffa95](https://github.com/juancavallotti/octo/commit/65ffa95afc4a305ae0311aa97a8ebb74a5f409e9))
* **editor:** derive breakpoint addresses from the document ([8dda8b6](https://github.com/juancavallotti/octo/commit/8dda8b68fa7b71283e3f3bf7a3df5e5d148a79c3))
* **editor:** editor-meta capability backed by .octo/editor-meta.json ([fe5b0f5](https://github.com/juancavallotti/octo/commit/fe5b0f516a639b1a1992783932a0d94f23a3db74))
* **editor:** flow-run results and console tabs ([7bbbbe8](https://github.com/juancavallotti/octo/commit/7bbbbe8097a27d79c0050107ce08ecc95ec3dd75))
* **editor:** one-shot flow invoke on the run transport ([2e5bd11](https://github.com/juancavallotti/octo/commit/2e5bd11016181459732ea28cb7b3ed1d822aa426))
* **editor:** run a flow from its card ([8e33ab3](https://github.com/juancavallotti/octo/commit/8e33ab3fdb645c4e0606d565725110d7dc8af099))
* **editor:** run flows, and run them to a breakpoint ([f47bcf0](https://github.com/juancavallotti/octo/commit/f47bcf0991ba3c9e15ca78ff8aff2e405ed49679))
* **editor:** run to a breakpoint from a block ([ca4c015](https://github.com/juancavallotti/octo/commit/ca4c01536493c7a2d9247d8bbabff701f0c2fc02))
* **engine:** add the implicit breakpoint composite ([8247634](https://github.com/juancavallotti/octo/commit/824763419504850a66ce9ebe4ae7ddacda68362c))
* **run-host:** expose break-at, vars and log level on invoke ([24e6e53](https://github.com/juancavallotti/octo/commit/24e6e534146f7e663bedda724a1fc030ee0e78a7))
* **runtime:** add WithBreakpoint to the invoke-mode service ([6f625c1](https://github.com/juancavallotti/octo/commit/6f625c19fcd08bfdab051c23a776b3a756803ea4))
* **runtime:** breakpoints — inspect a message mid-flow with invoke --break-at ([dd5334c](https://github.com/juancavallotti/octo/commit/dd5334cb36131be0db193d496a4543534647a325))
* **runtime:** pass variables to an invoked flow ([9b6b382](https://github.com/juancavallotti/octo/commit/9b6b382c73805bbc72827335eee8ac3384a9d568))
* **runtime:** resolve a breakpoint address into a flow config ([5085743](https://github.com/juancavallotti/octo/commit/5085743524b81c2b1dc8f3a269662fc98b91296c))


### Bug Fixes

* **docs:** render the CLI version from the release-please constant ([8db6392](https://github.com/juancavallotti/octo/commit/8db6392bb4e5b540cb56afacfdb8eea3014cb786)), closes [#139](https://github.com/juancavallotti/octo/issues/139)
* **editor:** typecheck the test suite ([f724dc8](https://github.com/juancavallotti/octo/commit/f724dc860421974b51429599ecbc37d11b255dde))
* **engine:** bubble a fork branch stop out to the parent message ([76a3ca1](https://github.com/juancavallotti/octo/commit/76a3ca1a58190f6d816fa20bde87a1ea93f28aa2))
* **engine:** do not cache a body whose flow requested stop ([c3f8099](https://github.com/juancavallotti/octo/commit/c3f80999d79eb2ff31d62a31b7f9c75da1a66e9f))
* **engine:** halt the ai-agent loop when a tool branch requests stop ([bf118df](https://github.com/juancavallotti/octo/commit/bf118df80c2cdf379ff8b3f1dc800c83de5b020f))
* **engine:** propagate an enrich body stop to the outer flow ([043c912](https://github.com/juancavallotti/octo/commit/043c9122df7e0b68067fec5dcf90a240a979d2d5))
* **standalone:** inject runtime schema into the /preview screenshot route ([3efe8f1](https://github.com/juancavallotti/octo/commit/3efe8f1036c76205789b11379a8ef9e0082ec04c))
* **standalone:** populate runtime schema in the /preview screenshot route ([1ce47c3](https://github.com/juancavallotti/octo/commit/1ce47c3b034a7bd98ecbc7d2486b74a56b8ad7db))


### Refactoring

* **editor:** make validation issues structured ([eae12b1](https://github.com/juancavallotti/octo/commit/eae12b14b76fc7b4fd5c335976f9f6587d05643b))


### Documentation

* document invoke --break-at and block addressing ([128bac4](https://github.com/juancavallotti/octo/commit/128bac4780a0ecebc9f961152eb02e6af6c9d648))
* document the CLI downloads and the runtime Docker image ([d31e8cd](https://github.com/juancavallotti/octo/commit/d31e8cd4531d0cc6b1eff16e335b4742e606a0f8))
* **editor:** document running and debugging flows ([f29e58d](https://github.com/juancavallotti/octo/commit/f29e58d8137446e6d2282d8c3071bbe7b27cc290))

## [0.3.7](https://github.com/juancavallotti/octo/compare/v0.3.6...v0.3.7) (2026-07-11)


### Features

* **docs:** bootstrapped docs by using fumadocs framework ([e7298e0](https://github.com/juancavallotti/octo/commit/e7298e07db7e9f70303a2c8ba2618156b17a9e81))
* **http-client:** retry 429 responses honoring Retry-After ([c73dd2f](https://github.com/juancavallotti/octo/commit/c73dd2f06796362e4cd43751ee3c38c990737fa4))
* **http:** global CORS + http-client 429 retry ([7b2da93](https://github.com/juancavallotti/octo/commit/7b2da930ed0f0d56389d9fc86ccc6510a2d07abf))
* **http:** global CORS, empty nil-body responses, typed response headers ([b5da294](https://github.com/juancavallotti/octo/commit/b5da294b20b6751bd0c4086f623659b0c0587762))


### Bug Fixes

* **mcp:** align zod on v4 to dedupe the MCP SDK instance ([05302aa](https://github.com/juancavallotti/octo/commit/05302aa0b8dccca7affb8411447622b7efd08b92))


### Documentation

* **concepts:** add Raw Content and Streaming page ([515b21c](https://github.com/juancavallotti/octo/commit/515b21c3380ba1f8ba0e09673696fedee67ca82e))
* rebuild the documentation site on Fumadocs ([1ffff23](https://github.com/juancavallotti/octo/commit/1ffff23b9c2604abc1c92e64b7c3cbce46181ae2))
* **site:** add live-platform screenshots for bus, architecture, sign-in, memory ([5e26183](https://github.com/juancavallotti/octo/commit/5e261831e2770ff52805e8f42c32e385ff300e75))
* **site:** add manual screenshots to platform, editor, and AI pages ([1f45aae](https://github.com/juancavallotti/octo/commit/1f45aaeb4c93c3dc96c7d828033a7dcab222d191))
* **site:** complete platform, deploy, and AI sections ([dbc55bc](https://github.com/juancavallotti/octo/commit/dbc55bc40385a8502f94044eb1d0e14eb47534fd))
* **site:** use the platform favicon ([9843a85](https://github.com/juancavallotti/octo/commit/9843a85e3bf07466a7b423de83cbddbac570b8f1))

## [0.3.6](https://github.com/juancavallotti/octo/compare/v0.3.5...v0.3.6) (2026-07-08)


### Features

* **cli:** add `octo schema` to emit the editor capability schema ([7f3b09a](https://github.com/juancavallotti/octo/commit/7f3b09a498e8ac50db45452fd4bcb22301c23d6f))
* **editor:** add CEL catalog, Prism grammar, and token theming ([42553b7](https://github.com/juancavallotti/octo/commit/42553b738d7bb8f89af91280f25e995e603d00b6))
* **editor:** add the notion connector and blocks to the schema ([34e12c6](https://github.com/juancavallotti/octo/commit/34e12c6e72efd4b59450841a88657f6b2765660e))
* **editor:** CEL autocomplete + highlighting for cel fields ([#125](https://github.com/juancavallotti/octo/issues/125)) ([8cde742](https://github.com/juancavallotti/octo/commit/8cde742a00c713da4275440c5cf85a67c0d36898))
* **editor:** CEL highlighting + autocomplete inside {{ }} templates ([#125](https://github.com/juancavallotti/octo/issues/125)) ([3f3af21](https://github.com/juancavallotti/octo/commit/3f3af21a7f665561d6a479a5dffb09e07aad88cd))
* **editor:** CEL highlighting, autocomplete & tester ([#125](https://github.com/juancavallotti/octo/issues/125)) ([e194c8e](https://github.com/juancavallotti/octo/commit/e194c8ed55e91598d73b2a20b970f76913644c88))
* **editor:** CEL tester enhancements — JSON highlighting, sample/receiver completion, auto-pairing ([#125](https://github.com/juancavallotti/octo/issues/125)) ([cbbed2a](https://github.com/juancavallotti/octo/commit/cbbed2af5f66a2773f8542c1a3d6ff974cde6da9))
* **editor:** CEL tester popup on the settings panel header ([#125](https://github.com/juancavallotti/octo/issues/125)) ([4567f97](https://github.com/juancavallotti/octo/commit/4567f97c74102578fac8ecc9cc703f535e8775c4))
* **editor:** drive capabilities from the runtime-generated schema ([2168c8c](https://github.com/juancavallotti/octo/commit/2168c8ce83803005d8f3a5dc05904374ce2548dc))
* **editor:** plumb one-shot CEL evaluation through the run transport ([6f17d3b](https://github.com/juancavallotti/octo/commit/6f17d3b5199c1f61610024c5e9baf7c32af4733a))
* **mcp:** serve the runtime schema from the binary ([cf60994](https://github.com/juancavallotti/octo/commit/cf60994d86f1cccf9a0d55b1c44fefc279f4488d))
* **notion:** add a block to retrieve page content blocks ([5b09e4c](https://github.com/juancavallotti/octo/commit/5b09e4c78848dc29b5063a667bb302c18cf32226))
* **notion:** add a Notion connector ([2f2844b](https://github.com/juancavallotti/octo/commit/2f2844b0a6935f1fef7312894b3212bd63f2abe4))
* **notion:** add retrieve-page and query-datasource blocks ([153fde9](https://github.com/juancavallotti/octo/commit/153fde9338abacb74ba772b0260485baac1483e4))
* **notion:** add the notion connector core to the runtime ([e2904a0](https://github.com/juancavallotti/octo/commit/e2904a03bcb96e4eb35576bc0c8ae81257611bb7))
* **notion:** add the page-to-markdown transformer block ([c6d7ab3](https://github.com/juancavallotti/octo/commit/c6d7ab3560b32d76f6b0276361a0b9dec782e627))
* **notion:** add webhook verify and event blocks ([447f776](https://github.com/juancavallotti/octo/commit/447f776b67f0755eae024c3dc484a2ff5afbd33f))
* **notion:** query a data source by database id ([c30abb9](https://github.com/juancavallotti/octo/commit/c30abb9a820fc17591eb320a37d11ff890397433))
* **schema:** add block/connector metadata registry in core ([c401e82](https://github.com/juancavallotti/octo/commit/c401e827eac423b0ef0118cdb97a24e8af5ee199))
* **schema:** add the capability-schema generator and octo tag walker ([f6d154d](https://github.com/juancavallotti/octo/commit/f6d154dadd0b56d8944b8f5436b883715f3f8a34))
* **schema:** describe logger, database, and the platform connectors from Go ([f597b60](https://github.com/juancavallotti/octo/commit/f597b60de6ff57e5e0b02cdf1df6a8a1e3267f8a))
* **schema:** describe the control-flow composites from Go ([60052b3](https://github.com/juancavallotti/octo/commit/60052b35e0a7a7039be6b7913c0b86b42b5e2db0))
* **schema:** describe the engine leaf blocks from Go ([2c2fd4f](https://github.com/juancavallotti/octo/commit/2c2fd4fde30716ae0e83efb52210a8c0aad8f808))
* **schema:** describe the http connector and its route source from Go ([2b7d746](https://github.com/juancavallotti/octo/commit/2b7d746d89329971f5c3b6581e58eb5ff3301282))
* **schema:** describe the http-client connector and rest block from Go ([42712d1](https://github.com/juancavallotti/octo/commit/42712d1c8cb4024fbe9b5aa7ebec4cb8610954c6))
* **schema:** describe the if composite via a schema-only meta struct ([61ae007](https://github.com/juancavallotti/octo/commit/61ae00734a05b70ecc1cf6d7ccedae9f815865f5))
* **schema:** describe the LLM connectors, ai-mapping, and jwt-validate from Go ([0f237a7](https://github.com/juancavallotti/octo/commit/0f237a7e1a8b8b0aa5562f37281d805bb5c14b17))
* **schema:** describe the notion connector and its blocks from Go ([a349b22](https://github.com/juancavallotti/octo/commit/a349b222a4b1bcadf912c5819635d4501873865d))
* **schema:** describe the set-payload block from Go ([04fae8f](https://github.com/juancavallotti/octo/commit/04fae8f00dca729bb2a3f739857308f77016f883))
* **schema:** describe the slack connector and its blocks from Go ([c7dedfa](https://github.com/juancavallotti/octo/commit/c7dedfa9d98623ee1f3b79000a67d54b5d9a5c5e))
* **schema:** fill field descriptions from Go doc comments ([683c810](https://github.com/juancavallotti/octo/commit/683c8108566d362cd738634ac8106c2c1644a107))
* **schema:** generate the editor capability schema from the Go runtime ([0cbcfd5](https://github.com/juancavallotti/octo/commit/0cbcfd514d35c1c1598ce69591973a9cd12f26e1))


### Bug Fixes

* **cli:** reword schema comment so it isn't read as a go:generate directive ([d6b9acd](https://github.com/juancavallotti/octo/commit/d6b9acdbe7f7e6e1c8b99771df00f93f4ac8dd72))
* **editor:** keep the selected CEL completion visible and steady during keyboard nav ([c6482f6](https://github.com/juancavallotti/octo/commit/c6482f6cde2d630507aef01642c8f3d2e925b814))
* **editor:** only auto-pair brackets/quotes inside CEL scope ([aca8817](https://github.com/juancavallotti/octo/commit/aca88173dcaec2c587f30cfef533e71f1c4b4cdc))
* **editor:** render CEL completion menu in a portal so overflow can't clip it ([f3cf8a6](https://github.com/juancavallotti/octo/commit/f3cf8a64c820742373004bee015f10e6749d1836))
* **editor:** suppress CEL autocomplete inside string literals ([3c011a7](https://github.com/juancavallotti/octo/commit/3c011a793deafe713b8d3b1ad66affc8ef8a53fd))
* **notion:** normalize the Notion-Version header ([3cb4470](https://github.com/juancavallotti/octo/commit/3cb44706935b6bec2f122184cda144106412dc0d))
* **platform:** inject the runtime capability schema into the editor ([3080c98](https://github.com/juancavallotti/octo/commit/3080c98b6588ad9d9061f1513d4c9d594b1b8cf9))


### Refactoring

* **editor:** ship an empty capability schema, generate the real one ([6df3e5b](https://github.com/juancavallotti/octo/commit/6df3e5b1baf3c8cc4b8ce6bdb3ee6f018a11fab7))


### Documentation

* **notion:** add a sample, mcp example, and connector docs ([efde49e](https://github.com/juancavallotti/octo/commit/efde49ec61d706e104bff09c864e2fcd9f5dff3e))
* **notion:** render page content by fetching its blocks first ([1d74816](https://github.com/juancavallotti/octo/commit/1d74816de22e19fdce952aa7b0455312df88c6a4))

## [0.3.5](https://github.com/juancavallotti/octo/compare/v0.3.4...v0.3.5) (2026-07-06)


### Features

* **engine:** add flow stop primitive for filter blocks ([e2eb7d8](https://github.com/juancavallotti/octo/commit/e2eb7d8eb8522e09d8341b9ac93de2a92222beed))
* **engine:** add generic validate filter block ([4eea906](https://github.com/juancavallotti/octo/commit/4eea906d5c00a433282eea1ef4679b69f4b8589d))
* flow-terminating filters — validate + jwt-validate (MCP-compliant auth) ([8b0946c](https://github.com/juancavallotti/octo/commit/8b0946cc48ef9512e4446dec6a406ad5d56151e2))
* **http:** add jwt-validate filter block with MCP-compliant auth ([e9fe170](https://github.com/juancavallotti/octo/commit/e9fe17001e480907436b61b932e1d392180f2010))

## [0.3.4](https://github.com/juancavallotti/octo/compare/v0.3.3...v0.3.4) (2026-07-05)


### Features

* **http:** default host/port from HTTP_HOST/HTTP_PORT env vars ([e4b7018](https://github.com/juancavallotti/octo/commit/e4b701886f72b9d6e4726306eaddbbdf365673f0))
* **mcp:** add getCelFunctions tool and CEL catalogue ([deb9a3e](https://github.com/juancavallotti/octo/commit/deb9a3eff5f9180af889192f3157a52b2cf4205c))
* **mcp:** serve schema and examples as tools, not just resources ([453e439](https://github.com/juancavallotti/octo/commit/453e439a731f1fd630fdd77cf153fdc24f4f6b8d))
* prod-readiness enhancements (http env defaults, slack raw-body [#104](https://github.com/juancavallotti/octo/issues/104), MCP docs tools) ([f16b2cf](https://github.com/juancavallotti/octo/commit/f16b2cf1902098a9239c25a994fa43d87001cfa4))
* **slack:** make slack-verify-request raw-body aware ([#104](https://github.com/juancavallotti/octo/issues/104)) ([b93776f](https://github.com/juancavallotti/octo/commit/b93776f24b8a2773f2807294a2a37f8f391844b6))

## [0.3.3](https://github.com/juancavallotti/octo/compare/v0.3.2...v0.3.3) (2026-07-05)


### Features

* **mcp:** authenticate /mcp as an OAuth 2.1 resource server ([2b6f5d2](https://github.com/juancavallotti/octo/commit/2b6f5d2e2f600f81b5fcfe86faa6676c23a5b8e2))
* **mcp:** OAuth 2.1 auth for the MCP endpoint ([52c37a4](https://github.com/juancavallotti/octo/commit/52c37a4645ec3df67f50dcf083ad09714fce2cf0))
* **mcp:** serve OAuth protected-resource metadata ([5562aa8](https://github.com/juancavallotti/octo/commit/5562aa812bf26ff77c32f08a84c63ed3e4361fcf))
* **platform:** advertise the MCP endpoint on the dashboard ([b5b5604](https://github.com/juancavallotti/octo/commit/b5b5604d22dbd4c8b4b322032c16e1ce9e0610e0))


### Bug Fixes

* **auth:** key web sign-in on the real OIDC subject ([0efdaec](https://github.com/juancavallotti/octo/commit/0efdaecd2ef5b6b57bd62acd19c7e1c66b1fb51f))
* **auth:** key web sign-in on the real OIDC subject ([66b6a0e](https://github.com/juancavallotti/octo/commit/66b6a0ebebad69b77adabded05a230280074ac6a))
* **mcp:** report a stable Octo server identity ([47e537d](https://github.com/juancavallotti/octo/commit/47e537d5e7fcebd088dccc62ae7dd27fef72bb3f))
* **mcp:** report a stable Octo server identity ([a9cf25f](https://github.com/juancavallotti/octo/commit/a9cf25ff344d78264b266378170bdfe4daac2440))

## [0.3.2](https://github.com/juancavallotti/octo/compare/v0.3.1...v0.3.2) (2026-07-05)


### Features

* **deploy:** let rollout edit env and gate required vars ([eb8138e](https://github.com/juancavallotti/octo/commit/eb8138ebbbd2557ccb97a5faec17672e3f14582c))
* **deploy:** rollout & editor deploy — tag-on-rollout, env editing, deployment selection ([2dcc76c](https://github.com/juancavallotti/octo/commit/2dcc76cf150b0b79574e80a67e57a2ce8368f433))
* **editor:** tag+env deploy dialog with deployment selection ([099ed63](https://github.com/juancavallotti/octo/commit/099ed63cf2332f5cefc2d368abc732dd06de5cb1))
* **integrations:** rollout dialog with tag-on-rollout and env editing ([7fcc7af](https://github.com/juancavallotti/octo/commit/7fcc7affa216a59246fcdc53a11fa10e7c0e0eab))


### Bug Fixes

* **deploy:** ignore empty env bindings so they don't clobber .env files ([bfcfea3](https://github.com/juancavallotti/octo/commit/bfcfea375bbdc08e03428f075e6ea7c70815639b))

## [0.3.1](https://github.com/juancavallotti/octo/compare/v0.3.0...v0.3.1) (2026-07-05)


### Features

* **integrations:** 2-column detail grid + compact deployments ([9ba1984](https://github.com/juancavallotti/octo/commit/9ba1984631357284523a2b12b2b384ba7826614a))
* **integrations:** active-version selector scopes resources to a tag ([410da92](https://github.com/juancavallotti/octo/commit/410da92398e6be483497e268324df723083e3ff2))
* **integrations:** attribute integration creator and last editor ([728a3cb](https://github.com/juancavallotti/octo/commit/728a3cb3f88f7b818284e37a8c3e0e2bcb428d10))
* **integrations:** enforce unique integration names ([c4f2a7b](https://github.com/juancavallotti/octo/commit/c4f2a7b9198ca1ce89a87c6f55c3a29aacd37cbe))
* **integrations:** keep rename editor open on duplicate-name conflict ([3d9f58f](https://github.com/juancavallotti/octo/commit/3d9f58fec97c702b4b5f5e71ddec2c2f54de7f52))
* **integrations:** per-pod dockable log panel ([#66](https://github.com/juancavallotti/octo/issues/66)) ([dca6ffe](https://github.com/juancavallotti/octo/commit/dca6ffe49db825265435b2eca7bd9030e052917e))
* **integrations:** source-type icon per integration in the list ([0a33d84](https://github.com/juancavallotti/octo/commit/0a33d847f4870676bd37019fa2871b0b78e2ed54))
* **integrations:** surface required env at deploy time ([#105](https://github.com/juancavallotti/octo/issues/105)) ([7ea85d4](https://github.com/juancavallotti/octo/commit/7ea85d4f02aeb24bb48fc9ff36c743f51c6ac66b))
* **integrations:** version-driven deploy UX + env-file aware deploys ([c19c36c](https://github.com/juancavallotti/octo/commit/c19c36c8028334311d3c5bbdc55f61507775c3a6))


### Bug Fixes

* **deploy:** keep OIDC auth alive across Cloud Build deploys ([07824d1](https://github.com/juancavallotti/octo/commit/07824d1b054fa8928a79cf019769bf3ca89c87af))
* **deploy:** keep OIDC auth alive across Cloud Build deploys ([386578b](https://github.com/juancavallotti/octo/commit/386578b117aff6706e964be210a179fa01a285c7))
* **rbac:** grant orchestrator pods/log and secrets access ([70e5bf8](https://github.com/juancavallotti/octo/commit/70e5bf890aa3d96a0866e0af218a72d11f7d8c01))

## [0.3.0](https://github.com/juancavallotti/octo/compare/v0.2.3...v0.3.0) (2026-07-04)


### ⚠ BREAKING CHANGES

* **types:** Message is no longer JSON-only by contract. Body may now hold a raw-content payload ({contentType, rawData}) when RawContent is true; consumers that assumed Body is always decoded JSON must account for the raw-content mode.

### Features

* **ai-agent:** bind resources as loadable skills ([c1c2afc](https://github.com/juancavallotti/octo/commit/c1c2afcc12d044dca6279227a2297a13acbe4c38))
* **cli:** add eval command to evaluate CEL expressions ([cda20a2](https://github.com/juancavallotti/octo/commit/cda20a280e3db22dd7f75f331aab25ca1267b49b))
* **editor:** add markdown support to resources ([ca4a1f2](https://github.com/juancavallotti/octo/commit/ca4a1f2e74612c907081239930c83ef98422945c))
* **editor:** add read-only Resources tab with scope reconciliation ([7946506](https://github.com/juancavallotti/octo/commit/7946506d5213b0623657f19b50f4f084f08012bf))
* **editor:** add ResourceStore capability for the Resources tab ([167556b](https://github.com/juancavallotti/octo/commit/167556bbea2ce7c6a6cc3e974177f7019a0f6bda))
* **editor:** add skills field to the ai-agent block ([8ebc3af](https://github.com/juancavallotti/octo/commit/8ebc3af593e251eba6a6f359fd141a2819946910))
* **editor:** add the mcp-router block with the MCP logo ([f82e269](https://github.com/juancavallotti/octo/commit/f82e269662767980374cd54e23fecbbf60cf9d55))
* **editor:** auto-select Logs tab when a run starts ([9f146b8](https://github.com/juancavallotti/octo/commit/9f146b83e725a37281c1661a80cd3f4fa06e194a))
* **editor:** back the Dev .env panel with the resource store ([4eec407](https://github.com/juancavallotti/octo/commit/4eec407fe97e1402f0ae2999b81a87dcd4dc1712))
* **editor:** edit resource content with syntax highlighting + autosave ([df705f0](https://github.com/juancavallotti/octo/commit/df705f0659598d1e11276fa726f5acc1cef51846))
* **editor:** expose rawBody/contentType in capabilities schema ([f1daf6f](https://github.com/juancavallotti/octo/commit/f1daf6f117ac8c8add700ea526d554023b07ba23))
* **editor:** highlight {{ }} expressions and use a monospace editor ([321038a](https://github.com/juancavallotti/octo/commit/321038a4b3455c7efbfcf1a48fbef7f939b1b9b9))
* **editor:** let flows grow beyond the canvas width instead of wrapping ([0bc94aa](https://github.com/juancavallotti/octo/commit/0bc94aa54a7b4f109ae377285c3768d793c50109))
* **editor:** manage resources from the tab — create, delete, scope ([6318743](https://github.com/juancavallotti/octo/commit/63187431e3ee5045546aba24ef6b00ef93d0a8b7))
* **editor:** move resources by drag via a provider move op ([f905f04](https://github.com/juancavallotti/octo/commit/f905f0402f4ebac3d28124c918738456d03df522))
* **editor:** persist console panel height to localStorage ([54e05bc](https://github.com/juancavallotti/octo/commit/54e05bc44c78216fce461e732f2f5020d2626e55))
* **editor:** Resources tab — file manager + syntax-highlighted editor ([be2f779](https://github.com/juancavallotti/octo/commit/be2f77960ba3a74a89ade85de32ad164e2acd32d))
* **editor:** show all .env.dev keys in Dev env panel with declare shortcut ([987d524](https://github.com/juancavallotti/octo/commit/987d524f5b315f976da8b8cc2984967a77960936))
* **editor:** surface template resources in the editor ([74cdabb](https://github.com/juancavallotti/octo/commit/74cdabb788e43713a433007dfac29f624cdba4cf))
* **editor:** thread the integration id through the run transport ([9e32886](https://github.com/juancavallotti/octo/commit/9e3288671cb0f1376f90347430aff0c9fd93f41f))
* **engine:** add rawBody option to set-payload and template-resource ([fbf23ff](https://github.com/juancavallotti/octo/commit/fbf23fffb2856b92658caba83b23b1e1b74727d9))
* evaluate CEL expressions via the runtime CLI and MCP ([6eca919](https://github.com/juancavallotti/octo/commit/6eca9197a9784f7086f8ab4875ac1e017c6bc87d))
* **expr:** add toFormData/fromFormData CEL functions ([760497f](https://github.com/juancavallotti/octo/commit/760497fd02356b3d7eb487fb3a8046c4c12a1164))
* **expr:** add toJson/fromJson CEL functions ([81bfd19](https://github.com/juancavallotti/octo/commit/81bfd198f9c7fe3d657763c41edd8f4b0efe9c83))
* **httpclient:** capture non-JSON rest responses as raw content ([8ad25da](https://github.com/juancavallotti/octo/commit/8ad25da78f78c43af05b0a136c1b037040680dea))
* **http:** serve and source raw-content bodies ([64ef480](https://github.com/juancavallotti/octo/commit/64ef4801b22d2b345a9b6d6f5e17740e450986dd))
* **mcp-router:** expose a flow as a stateless MCP server ([adfb5ef](https://github.com/juancavallotti/octo/commit/adfb5ef2b6242d402f562ca7114e4fef6d9568cb))
* **mcp:** add evaluate_cel tool to test CEL expressions ([47da071](https://github.com/juancavallotti/octo/commit/47da071ccd38d5296c0601ef9873e6ae7a0962b6))
* **mcp:** add raw-content example ([d3af832](https://github.com/juancavallotti/octo/commit/d3af832624c3eea80e3fc3fc2236b79711cd8adb))
* **mcp:** add resource-management port, tools, and env-var awareness ([47eaea4](https://github.com/juancavallotti/octo/commit/47eaea43cd79e82920a0197336552128bbe338ef))
* **mcp:** resource management + env-var awareness ([3df7f9f](https://github.com/juancavallotti/octo/commit/3df7f9f1f37304627ee8152826d965046243caf7))
* **mcp:** supply integration resources to run and invoke tools ([891af8f](https://github.com/juancavallotti/octo/commit/891af8f808d0a19f9a85a494036f0f39ee2b6682))
* **orchestrator:** add integration resource CRUD API ([8097629](https://github.com/juancavallotti/octo/commit/80976293bed833fd3afa0594ff712e2130b1f6b9))
* **orchestrator:** freeze resources on tag and serve them by snapshot ([4b8d47e](https://github.com/juancavallotti/octo/commit/4b8d47ecca264bd9256ad0a6730cc5603a0fce83))
* **orchestrator:** inject OCTO_SNAPSHOT_ID into deployed pods ([7f2add5](https://github.com/juancavallotti/octo/commit/7f2add578901bd6ccc6e5b6893ae038725f32690))
* **platform:** add a resources panel to the integration manager ([706e60b](https://github.com/juancavallotti/octo/commit/706e60bcf7c92c879a8b16cf219ac587c390ac1b))
* **platform:** add resource update/upsert to the orchestrator client ([063971a](https://github.com/juancavallotti/octo/commit/063971afd00c397840764283a5e78215557866aa))
* **platform:** back MCP resource tools with the orchestrator ([26cb313](https://github.com/juancavallotti/octo/commit/26cb31310bbf240de15e42d86bc6e179c0c0cab8))
* **platform:** back the editor Resources tab with the orchestrator ([6d066aa](https://github.com/juancavallotti/octo/commit/6d066aab6299ac8dd59459ffd14156ec9490da91))
* **platform:** propagate RawContent over NATS ([e3142b4](https://github.com/juancavallotti/octo/commit/e3142b4950501e536730b29ececd6c8acb4cba35))
* **platform:** resolve run resources from the orchestrator ([a820080](https://github.com/juancavallotti/octo/commit/a8200806eeb313b1751be9c4fd7e4ff7a029e5fb))
* **platform:** upload resource files instead of inline content ([aae4ddb](https://github.com/juancavallotti/octo/commit/aae4ddba171f974239e20726de192d5324973705))
* ResourceLoader — env resources & template resources ([4571cf7](https://github.com/juancavallotti/octo/commit/4571cf756f4b8e5147d7ad8f86016b088e55e67c))
* **run-host:** add evalCel to shell out to `octo eval` ([1a35425](https://github.com/juancavallotti/octo/commit/1a35425a92938c0c3b1a49c0e47ab0a32b4b0e8c))
* **run-host:** auto-inject the .env.dev resource into runs ([07c0508](https://github.com/juancavallotti/octo/commit/07c0508e33d14876635b423aced8eec42ec5fa33))
* **run-host:** stage declared resources into the run namespace dir ([de81dd8](https://github.com/juancavallotti/octo/commit/de81dd8e37c7297d93c603470c7e9303a361ba69))
* **runtime:** add ResourceLoader interface, standalone loader, and noop ([e17f45b](https://github.com/juancavallotti/octo/commit/e17f45b55a34a0eb71017deb05c7f403cf4c22c2))
* **runtime:** add resources config section and watch wiring ([f469d80](https://github.com/juancavallotti/octo/commit/f469d809ed129ab4faa635b1e12e8d18dfb8ad77))
* **runtime:** centralize message CEL and add templateResource(id) everywhere ([39d2cc2](https://github.com/juancavallotti/octo/commit/39d2cc21eefeb0c92147db862781d659a6f80558))
* **runtime:** combine env resources and add the template-resource block ([9baeb3a](https://github.com/juancavallotti/octo/commit/9baeb3a57f8fc8be5a19c0836e2cad6d906b81cb))
* **runtime:** declare template resources with optional alias, fail-on-deploy ([bd5f0d3](https://github.com/juancavallotti/octo/commit/bd5f0d3035c9be23d6f8f834b4605bc13e9c76e7))
* **runtime:** load k8s resources from the orchestrator by snapshot ([bcac578](https://github.com/juancavallotti/octo/commit/bcac57865e3c96ad9720bd1689725d2c2e7f7057))
* **sql:** add integration resource tables ([c5e3562](https://github.com/juancavallotti/octo/commit/c5e3562e5e00ac3b66c3261aeb80cc055b28a902))
* **standalone:** add a filesystem resource write path ([e1833a6](https://github.com/juancavallotti/octo/commit/e1833a622ad27af0c3e7b61758e419d23fadd398))
* **standalone:** back MCP resource tools with local disk ([fb093e4](https://github.com/juancavallotti/octo/commit/fb093e473ad86133fd3f39e838be06cb3360afac))
* **standalone:** back the Resources tab with local-disk files ([0083338](https://github.com/juancavallotti/octo/commit/0083338af1933571836bb47c572446c376289867))
* **standalone:** resolve run resources from the flows dir ([18772bc](https://github.com/juancavallotti/octo/commit/18772bca91d051eba53649e7f008ae41cf0f882b))
* supply integration resources to editor and MCP runs ([010d982](https://github.com/juancavallotti/octo/commit/010d982c8bfb66a4cc0dce1020d2f06558cb8846))
* **types:** add RawContent mode to Message ([3698b36](https://github.com/juancavallotti/octo/commit/3698b36b435eab4393d8d15a7a8170bfde2ffc79))


### Bug Fixes

* **cli:** wrap json.Marshal error in eval command ([cfc071a](https://github.com/juancavallotti/octo/commit/cfc071a95cff0723e85bf9ad70e008fdfa5bfe88))
* **editor:** add top padding to flow board so flows clear launcher chips ([97b2400](https://github.com/juancavallotti/octo/commit/97b240087bd41729c59c4dba138e24daa9fe7f7f))
* **editor:** show a placeholder for an unset required reference ([f240568](https://github.com/juancavallotti/octo/commit/f240568276b457376cb377d1afe5c4a565ebb4b9))
* **editor:** stop stripping the trailing slash off run-proxy URLs ([8004a23](https://github.com/juancavallotti/octo/commit/8004a23d4ea6ecf82277d372dcb5ca70cc1b1c1a))
* **expr:** silence ireturn on CEL binding functions ([25fa7e5](https://github.com/juancavallotti/octo/commit/25fa7e505280f10f11738dea47090d44606ae0f4))
* **mcp-router:** satisfy linters in mcp.go ([e6975b3](https://github.com/juancavallotti/octo/commit/e6975b30d4aa3a3396ec2c8591c6f27e21268655))
* **platform:** don't pull run-host into the client bundle ([56ea6f0](https://github.com/juancavallotti/octo/commit/56ea6f06275657e5a9be0cfacf3664cfac3c7b5b))
* **platform:** let run-proxy URLs bypass the OIDC session gate ([61808f6](https://github.com/juancavallotti/octo/commit/61808f63ec296687290d1c484cf63d7665336a72))
* **platform:** make resource store React Compiler-safe ([6a6b28e](https://github.com/juancavallotti/octo/commit/6a6b28ec2b74a1b07d3d5db84b699ded0931cb0f))
* **runtime:** satisfy golangci-lint ([378c100](https://github.com/juancavallotti/octo/commit/378c100498ae083d91837e42181bde4025c61c6a))


### Refactoring

* **runtime:** centralize CEL activation and source-payload compilation ([3cc621c](https://github.com/juancavallotti/octo/commit/3cc621cfd3cdcb265ad7ab0827df5ae1a9cfbe07))


### Documentation

* **ai-agent:** document skills with a sample and MCP example ([dac8dca](https://github.com/juancavallotti/octo/commit/dac8dca42127c0e425adbf059e83f230eccca41a))
* document raw-content mode and JSON/form CEL functions ([3e4901b](https://github.com/juancavallotti/octo/commit/3e4901ba7447dade856c0bc1a26386a01277a0ac))
* document resource staging and .env.dev for editor and MCP runs ([af48b39](https://github.com/juancavallotti/octo/commit/af48b39156ff62f34a1d7c661faf4754451cc9fe))
* document resources in the cloud ([08d02b3](https://github.com/juancavallotti/octo/commit/08d02b36f009e7b46b06588f119b5233d53ebfbe))
* document resources, the template-resource block, and templateResource() ([459fda9](https://github.com/juancavallotti/octo/commit/459fda90c8467b4c307d6d69d42d04b91158098c))
* **mcp-router:** document the block with a sample and MCP example ([bb17013](https://github.com/juancavallotti/octo/commit/bb170135e2084af7d39eea087014af39f7182b95))
* **samples:** add ai-quote-cache showing LLM + cache + transform over HTTP ([014ae41](https://github.com/juancavallotti/octo/commit/014ae41f9069b94ce3daf3f1320c28bc108096b6))
* **samples:** add raw-content example flow ([b744d80](https://github.com/juancavallotti/octo/commit/b744d80e91d01110cb491eeb173360f9bb6ed0a1))
* **samples:** add resources-demo showing env resources and templates ([b9feef0](https://github.com/juancavallotti/octo/commit/b9feef05aa84fc2ebeae72bd415cba00135a23b2))

## [0.2.3](https://github.com/juancavallotti/octo/compare/v0.2.2...v0.2.3) (2026-07-02)


### Features

* **events:** add events connector for broadcast pub/sub ([a364ea3](https://github.com/juancavallotti/octo/commit/a364ea371d211e2484d67df7b2ceb080380cc4b0))
* **mcp:** add invoke_flow and list_flows tools ([dfaac24](https://github.com/juancavallotti/octo/commit/dfaac24c8fa1ca5d011c86904922380fb76bb75d))
* **mcp:** add write-effective-integrations design prompt ([4c20362](https://github.com/juancavallotti/octo/commit/4c20362e938ef0dcc9e268fb49e11e148abcbbf9))
* **mcp:** run a single flow (output + logs) and add design-best-practices prompt ([90546dd](https://github.com/juancavallotti/octo/commit/90546dde78415df3af967f3afa662a120e2ea794))
* **platform:** add one-click Deploy button to the editor ([5f1a74d](https://github.com/juancavallotti/octo/commit/5f1a74d8fda815451ca6664728ce46753c655bf1))
* **platform:** download and upload integration YAMLs ([#61](https://github.com/juancavallotti/octo/issues/61)) ([3148a5d](https://github.com/juancavallotti/octo/commit/3148a5d156e10b9ee53cce6e3ec4ed4bbc9e9823))
* **platform:** duplicate an integration from the manager and editor ([81acc97](https://github.com/juancavallotti/octo/commit/81acc97be0d2294ad32b67689275facf5be0a9a5))
* **platform:** rename an integration from the file manager ([a533b34](https://github.com/juancavallotti/octo/commit/a533b3430f2818b80e69b44399764101ef855876))
* **platform:** rename file-manager "Open" button to "Edit" ([c42a71b](https://github.com/juancavallotti/octo/commit/c42a71bc9f0c58787b6eaac20407b5d550718e8a))
* **platform:** suggest next semver version tag in editor ([4970e09](https://github.com/juancavallotti/octo/commit/4970e0970649834acda3c62d2db53a7f046d573e))
* **platform:** suggest the next version tag in the file manager ([d0e849c](https://github.com/juancavallotti/octo/commit/d0e849c1d66aa21d8a27ff06c545c52fd634095f))
* **run-host:** add one-shot invoke() to run a single flow ([b74cbd7](https://github.com/juancavallotti/octo/commit/b74cbd7e9414564e4c5771c07903df285f4c5a6e))
* **runtime:** add multi-transform block ([a9061fe](https://github.com/juancavallotti/octo/commit/a9061fe20e9a9a9bcbb23ea84567099e62539d16))
* **runtime:** add object-read default and existsVar ([40d96dc](https://github.com/juancavallotti/octo/commit/40d96dc0f6f0e12a1b72efa6d5886c39b2195cea))
* **services:** add Topics broadcast pub/sub ([a4bc435](https://github.com/juancavallotti/octo/commit/a4bc435bd48d9c2c9b1ca0cb78663780d243881a))
* **slack:** look up users by id, not just email ([af6a67d](https://github.com/juancavallotti/octo/commit/af6a67d574b6c2dfa560cc3ef0604a2a7a6b81ca))


### Bug Fixes

* **deploy:** tolerate a missing release/oidc.json ([0d7144c](https://github.com/juancavallotti/octo/commit/0d7144cac2d0e3a5416bb944578b88ef90b58a9c))
* **deploy:** tolerate a missing release/oidc.json in Cloud Build ([5cb010f](https://github.com/juancavallotti/octo/commit/5cb010fc1467c825f3216d83107777e6efcb1e15))

## [0.2.2](https://github.com/juancavallotti/octo/compare/v0.2.1...v0.2.2) (2026-07-01)


### Features

* **editor:** add a read-only YAML preview toggle (closes [#60](https://github.com/juancavallotti/octo/issues/60)) ([2ce0758](https://github.com/juancavallotti/octo/commit/2ce07584b771f0874292d6411ee9ab4708ac735f))
* **editor:** add enrich, agent memory, and clear-agent-memory to schema ([40bd621](https://github.com/juancavallotti/octo/commit/40bd62170a5c30fdc07b2420ec3aabe2dc861752))
* **editor:** add the slack connector and blocks to the schema ([62a3d28](https://github.com/juancavallotti/octo/commit/62a3d2808a63fc80ee7c505e037d049dc00f16e0))
* **editor:** expose flow concurrency fields (workers/buffer/pool) ([864549b](https://github.com/juancavallotti/octo/commit/864549b79dea2612fd902b8ceb22ad4a27fa0efe))
* **editor:** group the component palette into collapsible sections ([c303942](https://github.com/juancavallotti/octo/commit/c303942436ffb88e3c178a0af3732bae5b15314c))
* **editor:** YAML preview, grouped palette, and flow concurrency fields ([d52b1c7](https://github.com/juancavallotti/octo/commit/d52b1c7f31df5247cf05d19c01b95f5fb2da16bf))
* **http:** capture raw request body into an opt-in variable ([7c9f280](https://github.com/juancavallotti/octo/commit/7c9f280133b4ace1dba519ed5ece5abca680b6f5))
* **runtime:** add enrich scope composite block ([01a7e7a](https://github.com/juancavallotti/octo/commit/01a7e7ae384f246162f12feab622c3242efc34b3))
* **runtime:** add per-thread memory to ai-agent ([29d46d4](https://github.com/juancavallotti/octo/commit/29d46d4c5798c7ba8d041d6c30fde27e37d554a0))
* **runtime:** default flow workers to 8 ([be7cc44](https://github.com/juancavallotti/octo/commit/be7cc445786980eb5c7dfabd9d0a83c4568681c3))
* **slack:** add a Slack connector to the runtime ([cdae976](https://github.com/juancavallotti/octo/commit/cdae97698f7a26c81969861ba89829fa18ef9c68))
* **slack:** add the receive path (verify-request and event blocks) ([fc0b505](https://github.com/juancavallotti/octo/commit/fc0b5056cce31e71b9d18c143409d83bb215298d))
* **slack:** add the slack connector core ([e155e60](https://github.com/juancavallotti/octo/commit/e155e600506efd5a23003f71f9eb88744bda07b9))
* **slack:** add the slack-send-message block ([607e09b](https://github.com/juancavallotti/octo/commit/607e09bc926da3389c0be4b56174c6d08329f763))
* **slack:** add user lookup, reaction, and message-update blocks ([0fb1e77](https://github.com/juancavallotti/octo/commit/0fb1e7798877c6243699ec69143124f394b14b60))


### Bug Fixes

* **editor:** render the http source rawBodyVar field ([a2da0ce](https://github.com/juancavallotti/octo/commit/a2da0cebd102021ac0a2f7820e5573d8137efbe2))


### Refactoring

* **runtime:** enrich propagates via CEL expressions ([93dcddb](https://github.com/juancavallotti/octo/commit/93dcddbdbb4e802c7c6f5f13294feb7272ebdba0))
* **slack:** leave the body intact in slack-verify-request ([d39438d](https://github.com/juancavallotti/octo/commit/d39438d74078a09d732f93b6977122d9f32fd836))


### Documentation

* cover ai-agent memory in connectors reference and homepage samples ([c026935](https://github.com/juancavallotti/octo/commit/c026935a93322d0ab834b9cae150e3243ba00e68))
* **runtime:** add enrich and agent-memory samples, docs, and MCP examples ([a088424](https://github.com/juancavallotti/octo/commit/a088424434b671de337d147162fcd707b8021cdd))
* **slack:** add the slack sample, MCP example, and connector reference ([5d27a94](https://github.com/juancavallotti/octo/commit/5d27a94eca15b925bc5d485708582c642f51b3fd))

## [0.2.1](https://github.com/juancavallotti/octo/compare/v0.2.0...v0.2.1) (2026-06-29)


### Bug Fixes

* **deploy:** track helm chart version via release-please ([5a5d142](https://github.com/juancavallotti/octo/commit/5a5d142d5d98ce848ee09796ff7f29fb3935ddec))
* **deploy:** track helm chart version via release-please ([05a9b62](https://github.com/juancavallotti/octo/commit/05a9b62dc081522a30d9c72c328d4ae55b8c7779))


### Documentation

* expand platform docs — clustering, monitoring, queues, blocks ([5088d85](https://github.com/juancavallotti/octo/commit/5088d85091c4bcbc7b96798c13f0f9e086386af5))
* restructure site + rewrite/expand platform docs ([ccd2000](https://github.com/juancavallotti/octo/commit/ccd2000e54d0422ef6e660c894015789144893c0))
* restructure site into a landing page + unified docs section ([c103cd8](https://github.com/juancavallotti/octo/commit/c103cd811dbb7f879287feaa99222af4666d5c4d))

## [0.2.0](https://github.com/juancavallotti/octo/compare/v0.1.8...v0.2.0) (2026-06-29)


### ⚠ BREAKING CHANGES

* RuntimeServices gained a Queues() method; every implementer must provide it.

### Features

* **connectors:** add platform queue source and queue-dispatch block ([6e38534](https://github.com/juancavallotti/octo/commit/6e385340a173a994e8d4929fc2158cb93d06dfbb))
* **core:** add queue runtime-service interfaces + standalone implementation ([7f202c2](https://github.com/juancavallotti/octo/commit/7f202c2568c0a47068287b0f06af21626cd45978))
* **core:** add TeeHandler and LogShipper for log fan-out ([a2469ee](https://github.com/juancavallotti/octo/commit/a2469ee859bf1be9b39a1d9f39214be51ad5acc3))
* **devspace:** run NATS in the local dev cluster ([9f61c5a](https://github.com/juancavallotti/octo/commit/9f61c5afff5d4b056271730c84f4a532d10e9ba8))
* **editor:** add Platform Queue source and queue-dispatch block to schema ([54706d9](https://github.com/juancavallotti/octo/commit/54706d9e30aaae4c681b10d78f3d0d7ed8f966b2))
* expose queues on RuntimeServices and inject NATS_URL into runtime pods ([0982f8f](https://github.com/juancavallotti/octo/commit/0982f8fbc2c7deffd64616d32b5d96529f5b3092))
* **helm:** add NATS broker as a StatefulSet ([3682397](https://github.com/juancavallotti/octo/commit/368239710c0bdc57be1d570c7d1ae58eb7025d0c))
* **logs:** add GET /logs query API with filters and paging ([e9526cf](https://github.com/juancavallotti/octo/commit/e9526cf73d0bd45ff203d6924cde47ff2af04c41))
* **logs:** consume internal.logs into Postgres ([5707557](https://github.com/juancavallotti/octo/commit/57075571277a472e7c6c931e84ea52c0f8c0716e))
* **logs:** filter logs by app name and version ([882d9ad](https://github.com/juancavallotti/octo/commit/882d9ad80bafe6dc37c37987f118587ce5b1257b))
* **logs:** scaffold log-aggregator service ([6bac8e8](https://github.com/juancavallotti/octo/commit/6bac8e81c96099657e802af52aae04823e93c79b))
* **orchestrator:** add user-facing object-store JSON facade with listing ([37fa333](https://github.com/juancavallotti/octo/commit/37fa33326a5745f22ee3cafe3bbbf7625a919ec2))
* **orchestrator:** browse object-store namespaces, clean up secrets ([033fb9b](https://github.com/juancavallotti/octo/commit/033fb9be1275ab1cb37500d70581d491d3453a03))
* **orchestrator:** publish deployment snapshots to NATS, drop in-process hub ([a617317](https://github.com/juancavallotti/octo/commit/a6173173f00ae3ed5c16dac6eb8ceb14b3c78534))
* platform queue blocks (source + queue-dispatch) ([a3372e2](https://github.com/juancavallotti/octo/commit/a3372e2f64537edba9b2c5a5b151af53ae603281))
* **platform:** add /platform/logs view ([0786c18](https://github.com/juancavallotti/octo/commit/0786c18f55bb64668b0ca798b6cf44d2fdea6b89))
* **platform:** add /platform/queues broker monitoring view ([895454e](https://github.com/juancavallotti/octo/commit/895454e71c3e27f5b6168134a573e89c8ab6c8cf))
* **platform:** add a dedicated Deployments page ([6c72444](https://github.com/juancavallotti/octo/commit/6c7244446628a364e2d589bd53b2fbe2571b3e42))
* **platform:** add icons to the management nav tabs ([ac500b0](https://github.com/juancavallotti/octo/commit/ac500b006df575479fa58b9b33afefe565e4bf74))
* **platform:** add logs data layer (client, action, model) ([d311676](https://github.com/juancavallotti/octo/commit/d31167633cf4a1da328f079ace3c2159f2f11b41))
* **platform:** add logs shortcut to the dashboard ([258e742](https://github.com/juancavallotti/octo/commit/258e742ef4082f2bb0ba543d6157f539075026b3))
* **platform:** add object-store BFF and shared listAllDeployments helper ([cfc2e70](https://github.com/juancavallotti/octo/commit/cfc2e70bbe2b8c689e1e3148d2996836a92d2cb6))
* **platform:** add queue-stats data layer over NATS monitoring ([3fb830c](https://github.com/juancavallotti/octo/commit/3fb830cbce7a244f223b35ae0079c39b233f4084))
* **platform:** add the Object Store page (view/edit/create/delete) ([4158cb0](https://github.com/juancavallotti/octo/commit/4158cb0e7f9dcae51e19c895a5b0709249a67fd1))
* **platform:** fan out integration-write events over NATS for editor reload ([750df40](https://github.com/juancavallotti/octo/commit/750df40695248cdbcca13a6a70b6346380da05a7))
* **platform:** filter logs by app name and version with autocomplete ([f08db55](https://github.com/juancavallotti/octo/commit/f08db55667f17fc644483c3e228099c0432e9ae2))
* **platform:** full-width logs view with tailing and URL filters ([6776c10](https://github.com/juancavallotti/octo/commit/6776c10059de8975f689f182d2ed79be90054766))
* **platform:** make the integrations manager bookmarkable ([1b2d09d](https://github.com/juancavallotti/octo/commit/1b2d09dabf2341867376c35ba60ba61e5e46adff))
* **platform:** make the integrations manager bookmarkable via the path ([0f20682](https://github.com/juancavallotti/octo/commit/0f206825b09fa265194667bfed1ba010212404af))
* **platform:** navigation restructure, object store & deployments pages, NATS deployment events ([97ed68c](https://github.com/juancavallotti/octo/commit/97ed68c53549348883ada622ba6c1b46e5c026dd))
* **platform:** pick a namespace in the object store ([3f556b8](https://github.com/juancavallotti/octo/commit/3f556b89f5db782e569812750bc6fdfe6e1b7e60))
* **platform:** return from the editor to the integration in context ([e10e3d4](https://github.com/juancavallotti/octo/commit/e10e3d41bfdf58cf7518a7f7ac746c4975d80ecc))
* **platform:** serve deployment-status SSE from NATS, drop orchestrator proxy ([c118210](https://github.com/juancavallotti/octo/commit/c118210f60ff594105c8fe4e4bc91a6e7424210f))
* **platform:** show app version as a pill in the logs table ([4e6b35e](https://github.com/juancavallotti/octo/commit/4e6b35e28c0dc670c9757eff57ca41375b327dfb))
* **platform:** show pods and scale on the deployments page ([ed0f12b](https://github.com/juancavallotti/octo/commit/ed0f12ba2e3915497fe1fda70fc58bdc3dc76006))
* **platform:** show queue destinations on the queues view ([f62192b](https://github.com/juancavallotti/octo/commit/f62192b68f11d199299d2476d33c9e51dfba7bc6))
* **platform:** show the section nav on every page, including the dashboard ([575b0c1](https://github.com/juancavallotti/octo/commit/575b0c1958f60479b4be93ded764036cebc9b351))
* **platform:** thread namespace through the object-store BFF ([4d29daf](https://github.com/juancavallotti/octo/commit/4d29dafdcf4cd3b651c9ae85e89fde2f4b2e714b))
* queues as a runtime service (request-reply + point-to-point) ([c4fc59b](https://github.com/juancavallotti/octo/commit/c4fc59b98a1918b55618240065487d8e58ca0c95))
* **runtime:** tee process and connector logs to the sink ([081f1ae](https://github.com/juancavallotti/octo/commit/081f1ae446aa4fc555efc2c5a266eebba3a61477))
* **services:** add NATS-backed queues for the k8s module ([13dbdc9](https://github.com/juancavallotti/octo/commit/13dbdc935bd103c6bc561a9c4f137201675955ad))
* **services:** ship runtime logs to internal.logs in k8s ([6bbb604](https://github.com/juancavallotti/octo/commit/6bbb60436b9a52200e56b060a32843873c101328))
* **sql:** add logs table and indexes ([a840b5a](https://github.com/juancavallotti/octo/commit/a840b5a4c8143db2a742e7f0234241cdc3605de5))
* stamp app name/version onto shipped logs ([ec9c976](https://github.com/juancavallotti/octo/commit/ec9c97649a56c691768c9ae663ebde4f5f6b65cb))


### Bug Fixes

* **platform:** render signed-in routes per request so the account tile shows ([5e9f2f3](https://github.com/juancavallotti/octo/commit/5e9f2f3521e4a55a35c5fd64a2cbeb9dde2da79b))
* **terraform:** avoid sensitive for_each in kv encryption block ([9538143](https://github.com/juancavallotti/octo/commit/95381433c540ad3ba7a3d3f4fcaf4bddc7e21bf1))
* **terraform:** avoid sensitive for_each in kv encryption block ([0e9abe4](https://github.com/juancavallotti/octo/commit/0e9abe4cb6c2b517b548bfe1e27ed9e816d2ceac))


### Refactoring

* **helm:** organize templates into per-service folders ([cbcbb44](https://github.com/juancavallotti/octo/commit/cbcbb44735779a24cb99acd806af5cc952c2d545))
* **platform:** split management into per-route header tabs ([d395e52](https://github.com/juancavallotti/octo/commit/d395e522e1b550ed8178b0e0d30a0c3f007a240a))


### Documentation

* document the platform queue source and queue-dispatch block ([7b6fc27](https://github.com/juancavallotti/octo/commit/7b6fc270e5f114b938ba3b389812c88f8d249a1f))
* **samples:** add queue-loadbalance example for the platform queue blocks ([e4d903b](https://github.com/juancavallotti/octo/commit/e4d903b596acbdaf9f558979b6572814946f722a))
* **site:** add State & clustering and MCP authoring guides ([e265e47](https://github.com/juancavallotti/octo/commit/e265e475afcd43093d4269171a03568222eba5d8))
* **site:** document state/cache blocks, OAuth2, version tags & global now ([58b84c4](https://github.com/juancavallotti/octo/commit/58b84c47a62d9472e581119312fcfbd7ea446b6b))
* **site:** document the 0.1.7–0.1.8 features (state, versioning, MCP) ([88a22d9](https://github.com/juancavallotti/octo/commit/88a22d98fe387e48fb49f37650b76fd507c2a0e2))
* **site:** refresh landing page for the 0.1.7–0.1.8 wave ([057ab50](https://github.com/juancavallotti/octo/commit/057ab5045117cae3e9a6ccb8fa8bd97b57e349e4))

## [0.1.8](https://github.com/juancavallotti/octo/compare/v0.1.7...v0.1.8) (2026-06-27)


### Features

* **core:** add an object-delete block ([7bcb535](https://github.com/juancavallotti/octo/commit/7bcb53571d5185115ffdab36418328be45116ae5))
* **editor:** add a copy button for the running test URL ([d9b4d5b](https://github.com/juancavallotti/octo/commit/d9b4d5bda7459625a3377256c88a322c9934b158))
* **editor:** live-reload the open file on an external write ([9d56a9a](https://github.com/juancavallotti/octo/commit/9d56a9a81249e9e5ac37aa96a413935cd7b17a58))
* **editor:** render scopes as a single compact box ([efba420](https://github.com/juancavallotti/octo/commit/efba420d2f81b1831128bf596f51cf5cb23f788c))
* **events:** add @octo/events in-process bus + SSE plumbing ([833c68a](https://github.com/juancavallotti/octo/commit/833c68ac52f6bd3fae70a383708173ee52fac3ec))
* **http:** add @octo/http fetch-to-result abstraction ([617a308](https://github.com/juancavallotti/octo/commit/617a308d0a40c59da812292fcb1e0f5c47779abd))
* **mcp:** add validate_definition tool ([b3990a7](https://github.com/juancavallotti/octo/commit/b3990a7b07ed94222b84262e88ea1d3d91d5b82c))
* **mcp:** handler factory via mcp-handler ([d5cd4b1](https://github.com/juancavallotti/octo/commit/d5cd4b18abf930397a78ebef87420ee473c3168a))
* **mcp:** integration CRUD tools ([453d396](https://github.com/juancavallotti/octo/commit/453d396867a7fc07ec34a4030dfaf92a0be8238d))
* **mcp:** point the authoring prompt at the docs ([81b4c7e](https://github.com/juancavallotti/octo/commit/81b4c7e532d7ca4567a18cf289afa5a4a397d529))
* **mcp:** run-control tools and per-session namespace ([e7d8a05](https://github.com/juancavallotti/octo/commit/e7d8a05ec96cb18219ecf86efce1bef23dda7535))
* **mcp:** runtime-schema resource and authoring prompts ([70677b4](https://github.com/juancavallotti/octo/commit/70677b4c34386478c54e09de3723f848ef4f45e3))
* **mcp:** scaffold @octo/mcp package ([be37192](https://github.com/juancavallotti/octo/commit/be371926230bb25b2cf86729278e6d9aec66840c))
* **mcp:** serve worked examples as resources ([e974123](https://github.com/juancavallotti/octo/commit/e9741236568116d8c1e38756db6a097e1fa203cd))
* **orchestrator:** add users and per-user API keys ([257a9bd](https://github.com/juancavallotti/octo/commit/257a9bd9a245596a0b30e486ff14d8f4eb240491))
* **platform:** add an Account API keys management page ([9e8fdf7](https://github.com/juancavallotti/octo/commit/9e8fdf7d690002571202d2d0ea165cb366753e96))
* **platform:** add API-key server actions and orchestrator client ([30fbee6](https://github.com/juancavallotti/octo/commit/30fbee6ab6c5fde87f890bfc91b86c318287a2d6))
* **platform:** bootstrap a user on sign-in and expose session.user.id ([a333971](https://github.com/juancavallotti/octo/commit/a3339713ec8e19b4115121ccb9c052d88afc414a))
* **platform:** disable delete for deployed version tags ([f1f6b9c](https://github.com/juancavallotti/octo/commit/f1f6b9c8bb0d652e335be795cc0942784e8c09c8))
* **platform:** expose MCP at /mcp behind per-user API keys ([716f85a](https://github.com/juancavallotti/octo/commit/716f85a3eab46be3b4dc4d3273c9221066052e2e))
* **platform:** expose the MCP server at /mcp behind an API key ([37d528b](https://github.com/juancavallotti/octo/commit/37d528bb09b633b4bbf9677c35b85a2483d62b7c))
* **platform:** high-level orchestrator client and auth gates ([2726130](https://github.com/juancavallotti/octo/commit/27261309de65ab7edd2c9c37848b4b8d640bba77))
* **platform:** publish + stream integration writes for live reload ([c25e8b3](https://github.com/juancavallotti/octo/commit/c25e8b3b6559fb0143e8b98ba6333edd059db5b4))
* reusable MCP server for Octo integrations (standalone) ([914945b](https://github.com/juancavallotti/octo/commit/914945b872565dc0d19b1988d15ddf27ebef22d2))
* **standalone:** expose the integration MCP server at /mcp ([520fa4f](https://github.com/juancavallotti/octo/commit/520fa4feaca87e36f0c1034722f78a0145fdf4c6))
* **standalone:** publish + stream integration writes for live reload ([1f373eb](https://github.com/juancavallotti/octo/commit/1f373eb6115992d960a80d7eac1c44bb46371954))


### Bug Fixes

* **deploy:** avoid sensitive values in helm_release for_each ([08d7466](https://github.com/juancavallotti/octo/commit/08d7466e79b38ac70ed6b744353952a6493a688d))
* **deploy:** avoid sensitive values in helm_release for_each ([16c484f](https://github.com/juancavallotti/octo/commit/16c484f45c027de1f98edc092690ea6d49570f94))
* **docker:** link @octo/mcp and @octo/http manifests for the image build ([87a9355](https://github.com/juancavallotti/octo/commit/87a93556820f7f0533d9ad115cda106417cef60c))
* **mcp:** treat validation as advisory, don't gate runs on it ([7719d77](https://github.com/juancavallotti/octo/commit/7719d779a031778f9bfbcf396925ba7117b14124))
* **platform:** orchestrator availability probe on non-JSON /healthz ([6f5010f](https://github.com/juancavallotti/octo/commit/6f5010ffcfbce303ec286aafe42142655dfd5c2f))
* **snapshot:** block deleting a tag that is currently deployed ([5db4e0e](https://github.com/juancavallotti/octo/commit/5db4e0ed68556705c59f82c4aa64616c45b9f0ce))


### Refactoring

* **platform:** availability via action; retire forward()/proxy() ([53b3df2](https://github.com/juancavallotti/octo/commit/53b3df2fc9173c349d88cad8f28dbf0933092cd3))
* **platform:** deployments via server actions ([df4055d](https://github.com/juancavallotti/octo/commit/df4055dea5fca948ca3702c63caebb094e21d022))
* **platform:** folders, integrations, snapshots via server actions ([634cf99](https://github.com/juancavallotti/octo/commit/634cf99a254b2a8dc79572ce30c77a320c203016))
* **platform:** run-control via server actions ([a5dc2cd](https://github.com/juancavallotti/octo/commit/a5dc2cd6ab0aa141e88299f449bf086a003eeecb))
* **platform:** secrets via server actions ([3584420](https://github.com/juancavallotti/octo/commit/3584420085ff49a77767b2dfe8b57ccfdbd48a1a))
* **standalone:** filesystem via server actions ([4befb8e](https://github.com/juancavallotti/octo/commit/4befb8e5ef66935e76d9544e5701d416a714fc62))
* **standalone:** run-control via server actions ([d4cf1bc](https://github.com/juancavallotti/octo/commit/d4cf1bc25fdc280c3b60ddd943b334d845fbc40c))


### Documentation

* prefer server actions over API routes for Next.js apps ([9b805e7](https://github.com/juancavallotti/octo/commit/9b805e761e0097dc0ce6159c0f2a579b72f9472d))

## [0.1.7](https://github.com/juancavallotti/octo/compare/v0.1.6...v0.1.7) (2026-06-26)


### Features

* **blocks:** cache-scope and invalidate-cache ([95df7cd](https://github.com/juancavallotti/octo/commit/95df7cd615852acdd0cde726646ea8a9be8b10c6))
* **blocks:** object-read and object-write blocks ([6ad428d](https://github.com/juancavallotti/octo/commit/6ad428d4b8f82914c6de8e677bd0de4a84fb0db1))
* **cli:** select and wire the runtime services module at startup ([734b52a](https://github.com/juancavallotti/octo/commit/734b52a974d430cca60cbe579dbf2e7f9a8e51e7))
* **core:** add runtime services interfaces (leader election + KV) ([be9e30d](https://github.com/juancavallotti/octo/commit/be9e30dde634c67fb2f7d7aae6b873f37d293058))
* **cron:** fire a schedule once across replicas via leader election ([db837d7](https://github.com/juancavallotti/octo/commit/db837d78469b2ae3e2184cfd67cf757192a2e13d))
* **deploy:** generate and wire the KV encryption key ([b3207b8](https://github.com/juancavallotti/octo/commit/b3207b8e70f8e231d8db279b3a80efea9ea11e5d))
* **deploy:** inject k8s runtime services into deployed pods ([8970f0a](https://github.com/juancavallotti/octo/commit/8970f0ab4ae84137354de70ef0324b1ccf6dea3f))
* **deploy:** require a version tag and deploy its frozen definition ([8f8ca5a](https://github.com/juancavallotti/octo/commit/8f8ca5aea4c131beeb0e7ca8f7c5f2033e81b027))
* **deploy:** roll out a live deployment between version tags ([2c3b061](https://github.com/juancavallotti/octo/commit/2c3b0610eef4cdf0d5d4de967a3814c9fcd1f889))
* **editor:** catalog the cache/object blocks and http-client OAuth2 auth ([1672008](https://github.com/juancavallotti/octo/commit/1672008a247f654308f161b8b4358e373734fc65))
* **expr:** expose now (evaluation time) to block CEL expressions ([b10c7de](https://github.com/juancavallotti/octo/commit/b10c7def1f79e7401e43a07347862f6290db8db5))
* **httpclient:** OAuth 2.0 client-credentials auth ([03f6c33](https://github.com/juancavallotti/octo/commit/03f6c33814f960ad6b65b80b52aa8a63bc5a338e))
* **integrations:** collapsible folder tree ([fe4e94c](https://github.com/juancavallotti/octo/commit/fe4e94cc597886b8906464417e35a044ebbe124e))
* **integrations:** drag integrations into folders & reparent folders ([8cc77ee](https://github.com/juancavallotti/octo/commit/8cc77ee073a5eaf5e7d34990b53c1108b4adfb4e))
* **integrations:** reorder folder siblings ([91fa86a](https://github.com/juancavallotti/octo/commit/91fa86a671a2e07ec0d51f1dcf7c15357d8f4a3e))
* **integrations:** reorder integrations within a folder ([b478651](https://github.com/juancavallotti/octo/commit/b478651e4b691f6723839c61a198d7078be53073))
* **orchestrator:** deployment-scoped KV store with encrypted secret namespaces ([c84262c](https://github.com/juancavallotti/octo/commit/c84262c7c353d7b8b663a17b4baab3e6823e419f))
* **platform:** always show an account indicator in the header ([5952f57](https://github.com/juancavallotti/octo/commit/5952f574102361791f428137d359d17d85a8b271))
* **platform:** rollout control, in-app confirm dialog & integrations UX polish ([19c2bf5](https://github.com/juancavallotti/octo/commit/19c2bf5f190353b3d5678f9240bcb825070ae461))
* runtime services — keyed leader election + KV store ([5125144](https://github.com/juancavallotti/octo/commit/51251440100457675988832960b8679ec07186f9))
* **runtime:** inject runtime services into the execution context ([505e09e](https://github.com/juancavallotti/octo/commit/505e09e1f10d9edb96f8164547c560e5366965e3))
* **services:** add a secret store over the KV store via secret namespaces ([8b549a4](https://github.com/juancavallotti/octo/commit/8b549a4b758ba49a0810a8d3ad1f00fac78332ba))
* **services:** add k8s runtime services provider ([04f8c08](https://github.com/juancavallotti/octo/commit/04f8c08a139238cdc2ff1d21e99257705753e243))
* **services:** standalone runtime services provider + selection registry ([e155aca](https://github.com/juancavallotti/octo/commit/e155aca1b7eaa3d0a502e35ca4ff2b01ca26d033))
* **snapshots:** create/list/delete version tags from the page & editor ([79b66fd](https://github.com/juancavallotti/octo/commit/79b66fd12e53177a4f23c2473248909fb03bf871))
* **snapshots:** integration snapshot table & orchestrator module ([90becaf](https://github.com/juancavallotti/octo/commit/90becaf48758909dd8b07e18c1e9cebcaebc6632))


### Bug Fixes

* **blocks:** read cache-scope key/ttl as block fields, not settings ([1e808a1](https://github.com/juancavallotti/octo/commit/1e808a122e98516fc53b9d3fe53524d1fa0a0635))
* **deploy:** refresh octo-pull from metadata on every boot ([fa0a873](https://github.com/juancavallotti/octo/commit/fa0a873baf60b480a427916f4061b60f4aa31ad3))
* **deploy:** strip gcloud chatter from the fetched kubeconfig ([ffa1ea3](https://github.com/juancavallotti/octo/commit/ffa1ea309624c212b17843ad874d95e9468cf456))


### Refactoring

* **core:** export NoopLeaderElection and reuse it in standalone ([cdf444f](https://github.com/juancavallotti/octo/commit/cdf444fbee16b30d1f320da94f66923b70568a24))
* **core:** namespace KV keys and add preset system/user namespaces ([aa48a43](https://github.com/juancavallotti/octo/commit/aa48a43ae6f3f2eec034e31af3c755c4b6a0b6cd))


### Documentation

* **samples:** runtime-services demo flow ([6651e4d](https://github.com/juancavallotti/octo/commit/6651e4dcd0fab68dd9293fb5bfb0a8188741f356))

## [0.1.6](https://github.com/juancavallotti/octo/compare/v0.1.5...v0.1.6) (2026-06-22)


### Bug Fixes

* **ci:** trigger release build from release-please + add pnpm to release job ([7ce2010](https://github.com/juancavallotti/octo/commit/7ce2010d79b152820074dac84d4de73669b81251))
* **deploy:** SSH to the VM as a non-root user from Cloud Build ([7f73255](https://github.com/juancavallotti/octo/commit/7f732555188ca290f2e37b93df693f35254a273c))


### Refactoring

* **deploy:** rename deployed workload + image editor → platform ([182dee6](https://github.com/juancavallotti/octo/commit/182dee6c06d5267989a93a195e7ea07b0fe184e6))

## [0.1.5](https://github.com/juancavallotti/octo/compare/v0.1.4...v0.1.5) (2026-06-22)


### Features

* **platform:** unified navigation — welcome, dashboard, consistent chrome ([c1e450d](https://github.com/juancavallotti/octo/commit/c1e450d92c0c9b3f698baf25527f829bc22086b7))
* **standalone:** add Octo branding logo and favicon ([56f8772](https://github.com/juancavallotti/octo/commit/56f8772dfd7951eb5008cb3d0d30951db7182f75))
* **standalone:** local-disk filesystem (open/save flows) ([b3e3d4d](https://github.com/juancavallotti/octo/commit/b3e3d4d0bc871e57aac7c8558eaa4c74b0b6c795))
* **standalone:** public Docker image + release publish ([f0b0d1c](https://github.com/juancavallotti/octo/commit/f0b0d1cac7ca5b31d96603fd0fb3efc1b7e68b42))
* **standalone:** real file management — name→filename, rename, new, save-opens ([0522388](https://github.com/juancavallotti/octo/commit/0522388e385bb86d685167388f8cf13ee6c1d12a))
* **standalone:** scaffold local app embedding the editor + run ([1e6fd3f](https://github.com/juancavallotti/octo/commit/1e6fd3f0664faa7ddb50225a546aa4dd24007baa))


### Bug Fixes

* **deploy:** make the Cloud Build deploy step self-sufficient ([2c5a71d](https://github.com/juancavallotti/octo/commit/2c5a71dd2dec0dac3ec704e56448e281b75f0fae))
* **deploy:** make the Cloud Build deploy step self-sufficient ([cd582cb](https://github.com/juancavallotti/octo/commit/cd582cbc9ca057895c34d44b1ce3e640976a07fb))
* **site:** render inline markdown in the changelog feed ([307d7c0](https://github.com/juancavallotti/octo/commit/307d7c0ca26b72f84c0355e4e658c831087f5048))


### Refactoring

* **build:** point Docker/CI/deploy at apps/platform with pnpm ([0f756fd](https://github.com/juancavallotti/octo/commit/0f756fd2aaa4ba9db29929f92d112c7dbdd380a3))
* convert to pnpm workspace and move editor to apps/platform ([4042fd6](https://github.com/juancavallotti/octo/commit/4042fd6af46f0197c2a36ac64dcbb156a91df32c))
* **editor:** carve packages/editor reusable library ([7515bf6](https://github.com/juancavallotti/octo/commit/7515bf667e11b41ccd9a88d4b75dca0957e4277c))
* **editor:** make fs/run capabilities optional via EditorRoot ([f778677](https://github.com/juancavallotti/octo/commit/f778677fab154e0f52d0e802f0c3308db61a9eac))
* **fs:** route load/save through a FileSystemCapability ([4d18210](https://github.com/juancavallotti/octo/commit/4d182104714b224554caf9098301dba4ec4e5b26))
* **run:** extract @octo/run-host shared package ([9122fbd](https://github.com/juancavallotti/octo/commit/9122fbdfd45fbcbb9d36bf0873469109496dbf89))
* **run:** inject a RunTransport into RunProvider ([09f3861](https://github.com/juancavallotti/octo/commit/09f38611b1a5992ccb20fa3c3c860dc14ab3c925))
* **standalone:** move /preview + screenshot e2e; task dev -&gt; standalone ([2ef01b4](https://github.com/juancavallotti/octo/commit/2ef01b4bb6b8586f990526e47fd010522321ed93))


### Documentation

* refresh landing page + add Connectors/CEL/Error-handling/Deployment guides ([0aac8a5](https://github.com/juancavallotti/octo/commit/0aac8a522c1a7f9dc0d62ec4694cb093a9c1875c))
* **site:** add Connectors, CEL, Error handling & Deployment guide pages ([dffb2f2](https://github.com/juancavallotti/octo/commit/dffb2f259feb4c4834c192b9e50067571e38d6d3))
* **site:** add editor flow screenshots to What's New ([939f0db](https://github.com/juancavallotti/octo/commit/939f0dbc5bb9a351d0d4b0905ec25e8122c62fc7))
* **site:** add What's New, CEL cheat sheet, AI/error samples, logo ([628f24f](https://github.com/juancavallotti/octo/commit/628f24f7c2ef7bca63793258f16a08c3701cdd4f))
* **site:** cache-bust app.js so the changelog markdown fix loads ([56fa760](https://github.com/juancavallotti/octo/commit/56fa7600c243dcadb1bf4f55b0a9bb342c9cc7b0))
* **site:** document the deploy workflow, k8s platform, refresh roadmap ([0784468](https://github.com/juancavallotti/octo/commit/0784468895cfa108246761dcd5b37bca2a42cd85))
* **site:** run the built ./bin/octo, not `go run`, in quickstart & samples ([5551f35](https://github.com/juancavallotti/octo/commit/5551f357a8b40747c9681fd44452be02c645098c))
* **site:** show an editor screenshot in every sample + fix layout ([11ded96](https://github.com/juancavallotti/octo/commit/11ded964c37a2332d540f213e3b39b3be328824c))
* **site:** sync stale hero version badge to 0.1.4 ([7bcff4c](https://github.com/juancavallotti/octo/commit/7bcff4c8d73d782d17a22f73b42ca4d0679bf93e))

## [0.1.4](https://github.com/juancavallotti/octo/compare/v0.1.3...v0.1.4) (2026-06-21)


### Features

* **connectors:** add ai-mapping leaf block ([dd9ec5f](https://github.com/juancavallotti/octo/commit/dd9ec5f3bfb29e8bfa29840212cb87ee80088d2e))
* **connectors:** add llm-anthropic connector ([46eb024](https://github.com/juancavallotti/octo/commit/46eb0242180f2cfdaf6b09b017ea07dcf23b596d))
* **connectors:** add llm-gemini connector ([9986b47](https://github.com/juancavallotti/octo/commit/9986b47406fe249040d786e25a5d4cdd723b4f32))
* **connectors:** add llm-openai connector ([31618a9](https://github.com/juancavallotti/octo/commit/31618a94c008a18f964be626e9b7804106140793))
* **connectors:** update default LLM models ([2617cc7](https://github.com/juancavallotti/octo/commit/2617cc7b5da729ed21d8b340e740efd692e2cb2b))
* **core:** add provider-agnostic LLMClient interface + DTOs ([360ef71](https://github.com/juancavallotti/octo/commit/360ef719b7e27c74d50f9495f63122a9a85db8c5))
* **deploy:** wire editor OIDC SSO and drop Secret Manager ([d4c28fd](https://github.com/juancavallotti/octo/commit/d4c28fd30fb2debee0b2f1ff229788716a62e700))
* **editor:** add LLM connectors + ai-mapping/ai-retry to capabilities ([1b7746a](https://github.com/juancavallotti/octo/commit/1b7746a18e91d50a5c2be8f23bae014101c89592))
* **editor:** flow-level error path lane + canvas polish ([74db3e4](https://github.com/juancavallotti/octo/commit/74db3e4f7d6c53c6e84fcf52ebcd0e6f24123e11))
* **editor:** OIDC SSO via Auth.js with role-gated BFF routes ([28d5860](https://github.com/juancavallotti/octo/commit/28d5860047bd3c7629cec4cd62e9a50def23e806))
* **editor:** replace scope block with handle-errors ([b674f16](https://github.com/juancavallotti/octo/commit/b674f164068582e7caa8223172fc0f7daa43a6f3))
* **editor:** route-list/tool-list field types for ai-router & ai-agent ([e15d4ff](https://github.com/juancavallotti/octo/commit/e15d4ffee850b14a19b9afbda618fc6a081ca16c))
* **editor:** seed a starter inputSchema for new ai-agent tools ([393b0da](https://github.com/juancavallotti/octo/commit/393b0daa8220cf91ed80c1a52e14f7ff6939a146))
* **editor:** tabbed console with Dev .env values ([766817c](https://github.com/juancavallotti/octo/commit/766817c9cbd9ca301e463ba305ab68d29047edef))
* **engine:** add ai-agent composite ([a6bcbc9](https://github.com/juancavallotti/octo/commit/a6bcbc93fef0e4253a052777e5e694e1963ea606))
* **engine:** add ai-retry composite ([80c2159](https://github.com/juancavallotti/octo/commit/80c21593c2298ee48cec9184e89d0d81cdd0606a))
* **engine:** add ai-router composite ([cbb4779](https://github.com/juancavallotti/octo/commit/cbb47793b22e1f0c560e704c6a1ee2a46a6bf779))
* **http:** propagate vars.httpStatus as the response status ([b808fa1](https://github.com/juancavallotti/octo/commit/b808fa13f2c85ebc159055c8cbee0a4bfdb42c45))
* **log:** include variables in the default log line ([766036e](https://github.com/juancavallotti/octo/commit/766036e9e7872719cb63c98614f1bf9fb28a529e))
* **runtime:** add flow-level error path with recovery ([578e7ef](https://github.com/juancavallotti/octo/commit/578e7efe26bc259e0aa975176eb1f35c7173530f))
* **runtime:** add handle-errors block with structured vars.error ([cca5660](https://github.com/juancavallotti/octo/commit/cca5660adc02269b74fa7038939e55298321a2ea))
* **types,engine:** add BlockConfig AI fields and slot tracking ([f29364b](https://github.com/juancavallotti/octo/commit/f29364b0038abe2db737d60fb4dcd8fce6606b98))


### Bug Fixes

* **ai:** self-healing ai-retry, component LLM logging, route/tool decode ([1003daf](https://github.com/juancavallotti/octo/commit/1003daf7e9f4a809cababd88d53d6b6f287ee7cf))
* **ci:** extract terraform with python3 instead of unzip in deploy step ([3aba0e2](https://github.com/juancavallotti/octo/commit/3aba0e2019b0679709e8a35d34cbfc37dc43c443))
* **ci:** extract terraform with python3 instead of unzip in deploy step ([cad4ab1](https://github.com/juancavallotti/octo/commit/cad4ab1c6196ef8dbe8950f2404add6603757e7e))
* **deploy:** don't rotate adopted Postgres password on import ([3f91f33](https://github.com/juancavallotti/octo/commit/3f91f333ce1de1c9b09bcb435e0c409ace551b71))
* **deploy:** don't rotate adopted Postgres password on import ([6a2f678](https://github.com/juancavallotti/octo/commit/6a2f6786bb82184993262fa68c6e5c6a09192bb6))
* **gemini:** round-trip Gemini 3.x thought signatures in tool loops ([48f38a2](https://github.com/juancavallotti/octo/commit/48f38a2b914b706cefec28c0d06cc7e8371cfeba))


### Documentation

* document error handling + add error-handling sample ([2e8bea9](https://github.com/juancavallotti/octo/commit/2e8bea991f17b55d8bf91f110864c31b0e7a12c9))

## [0.1.3](https://github.com/juancavallotti/octo/compare/v0.1.2...v0.1.3) (2026-06-20)


### Features

* **deploy:** wildcard TLS cert via DNS-01 for integration subdomains ([#37](https://github.com/juancavallotti/octo/issues/37)) ([df99674](https://github.com/juancavallotti/octo/commit/df9967417edbdfedcdfbf4086d433fae6cd9778d))
* **editor:** allocate a port and inject HTTP_PORT for networked runs ([#36](https://github.com/juancavallotti/octo/issues/36) 2/5) ([86627f2](https://github.com/juancavallotti/octo/commit/86627f2ee9fce14dacdeaff4fab11bf7b7c418d1))
* **editor:** namespace editor runs per user ([#36](https://github.com/juancavallotti/octo/issues/36) 1/5) ([443247e](https://github.com/juancavallotti/octo/commit/443247eba505573a011ce7479b09a13712bba209))
* **editor:** reap idle namespaced runs after 1h ([#36](https://github.com/juancavallotti/octo/issues/36) 5/5) ([114c4da](https://github.com/juancavallotti/octo/commit/114c4da282f4043b8f40dff6693179f0e45927ea))
* **editor:** reverse-proxy networked runs at /editor/runs/&lt;ns&gt;/ ([#36](https://github.com/juancavallotti/octo/issues/36) 3/5) ([82e2f16](https://github.com/juancavallotti/octo/commit/82e2f163553ef29ee0053de7f8c076bc0a440b20))
* **editor:** surface the run test URL in the log panel ([#36](https://github.com/juancavallotti/octo/issues/36) 4/5) ([7477b89](https://github.com/juancavallotti/octo/commit/7477b899bdaa9d27d65a05b70209af8bfc7f044d))
* **runtime:** expose declared env vars to CEL as env.NAME ([#34](https://github.com/juancavallotti/octo/issues/34)) ([8e7e48c](https://github.com/juancavallotti/octo/commit/8e7e48cc988e29eebe2b2813116632999701d08b))


### Bug Fixes

* **helm:** grant orchestrator RBAC to manage secrets ([#33](https://github.com/juancavallotti/octo/issues/33)) ([72e60de](https://github.com/juancavallotti/octo/commit/72e60de8ea01a5dd1f86cf52419ebe4c180718aa))
* **http:** release listener on Stop to fix hot-reload port leak ([#22](https://github.com/juancavallotti/octo/issues/22)) ([7c66b56](https://github.com/juancavallotti/octo/commit/7c66b56d93f2f5bf2fb6ae2ff4ac432f8c80d9f4))
* **infra:** order deploy secret IAM grant after the secret exists ([648d3a0](https://github.com/juancavallotti/octo/commit/648d3a0c99a264413e93e4322b3edf59a23db7de))


### Refactoring

* **deploy:** collapse to a single octo.tfvars; drop bootstrap root ([6b3febd](https://github.com/juancavallotti/octo/commit/6b3febd584e50f3596e00986f8a4273946856f02))
* **deploy:** one combined infra root + Cloud Build-driven releases ([d905733](https://github.com/juancavallotti/octo/commit/d9057333845e94bfa036c060fb761bc3802aea00))

## [0.1.2](https://github.com/juancavallotti/octo/compare/v0.1.1...v0.1.2) (2026-06-20)


### Features

* **cluster:** add DevSpace dev mode with hot reload and log tailing ([158dcc5](https://github.com/juancavallotti/octo/commit/158dcc52d44823fb8b340843205629c33accaecb))
* **cluster:** add local k3d dev cluster via DevSpace ([7d65b9f](https://github.com/juancavallotti/octo/commit/7d65b9fced2dad4c9c8bb0c11ba7bd68767eca00))
* **cluster:** local k3d dev cluster via DevSpace (editor+runtime, orchestrator, sql) ([e9cf2a2](https://github.com/juancavallotti/octo/commit/e9cf2a2bf36937e030ab929a9082e60c80618a18))
* deploy integrations as Kubernetes pods ([f1d9f7d](https://github.com/juancavallotti/octo/commit/f1d9f7d1bded654686b7e3e5a6f8d5f1c9ea0cd3))
* **deploy:** Helm chart, Artifact Registry + Cloud Build for GCP ([b81e9aa](https://github.com/juancavallotti/octo/commit/b81e9aa32bd2f3687293271bd0581b45329ac840))
* **deploy:** single-node k3s VM + Terraform-owned Helm release ([bd5f9f0](https://github.com/juancavallotti/octo/commit/bd5f9f0681e96e36f5fc07311288f7067984e268))
* **editor:** bookmarkable /i/[id] route for opening integrations ([5e74c70](https://github.com/juancavallotti/octo/commit/5e74c7087887f3c83f254e75398acd12aabf3918))
* **editor:** deploy and manage integrations from the management UI ([dc0d992](https://github.com/juancavallotti/octo/commit/dc0d992442183f833cbc93a34a6617e67baa5707))
* **editor:** deployment UX — formatting, icons, modal, and scaling ([815700b](https://github.com/juancavallotti/octo/commit/815700bd84eb66e43752916fdde2f8040bbd26cc))
* **editor:** editable title, folder picker and save in title bar ([9705b1b](https://github.com/juancavallotti/octo/commit/9705b1b3ee16b917f17fc0f407484b8d08ed6ecf))
* **editor:** integrate orchestrator integrations & folders ([6dae196](https://github.com/juancavallotti/octo/commit/6dae196c15cc31ccce41c3859eba52495a9682bd))
* **editor:** integrations management route with folder CRUD and detail panel ([a29c13d](https://github.com/juancavallotti/octo/commit/a29c13d7eb1346d3f10dc5c075d7bbb5120c4561))
* **editor:** live deployment status over SSE with a polling fallback ([e3afb57](https://github.com/juancavallotti/octo/commit/e3afb577ed8fd64d535bb9a7849b9702bc18b642))
* **editor:** orchestrator API client and proxy routes ([94e0504](https://github.com/juancavallotti/octo/commit/94e0504f5b102d84f1c32c490b0325548ec8978d))
* **editor:** save the integration with Cmd/Ctrl+S ([4306e07](https://github.com/juancavallotti/octo/commit/4306e079794f96d4da6b870ac0e757db11e91558))
* **editor:** user-chosen deployment slug with live validation ([af17454](https://github.com/juancavallotti/octo/commit/af1745435c5de8018d679be9cce025342f8e3ae7))
* **orchestrator,editor:** external per-integration subdomains ([6dfb6d0](https://github.com/juancavallotti/octo/commit/6dfb6d087cd472675e77918a0c3af199c5b01b73))
* **orchestrator,editor:** richer deployment status ([2451c4f](https://github.com/juancavallotti/octo/commit/2451c4ff4e81ed6e65bc2f25701f4b06a4092657))
* **orchestrator:** add integration repository layer ([7f24c0e](https://github.com/juancavallotti/octo/commit/7f24c0eed16995f129d609f96068622cf7fea4f1))
* **orchestrator:** add integration REST endpoints ([7991942](https://github.com/juancavallotti/octo/commit/7991942c25412bff8d75a550e635aadc2436a21b))
* **orchestrator:** add integration service layer ([8932127](https://github.com/juancavallotti/octo/commit/8932127f7b1f59e051f9c3d7d50757011ba5ac76))
* **orchestrator:** add integrations datamodel to schema ([8712255](https://github.com/juancavallotti/octo/commit/8712255042ec56ec8d60a7d8b0873b0f93d0dd70))
* **orchestrator:** deploy integrations as Kubernetes workloads ([3eae392](https://github.com/juancavallotti/octo/commit/3eae3923dde52d4fb990c89b1521b159936eae18))
* **orchestrator:** folder HTTP API and route wiring ([b5b9544](https://github.com/juancavallotti/octo/commit/b5b9544c284371402db0eb54b24448945ce8d713))
* **orchestrator:** folder repo, types and DB tests ([61f333e](https://github.com/juancavallotti/octo/commit/61f333e1844b423fb4183fb43a3c00b95b016fd0))
* **orchestrator:** folder service with validation and tree assembly ([d6cec19](https://github.com/juancavallotti/octo/commit/d6cec1954970a085521b92c741f8593683a720d9))
* **orchestrator:** HTTP_PORT drives external exposability + port wiring ([0fbb9c2](https://github.com/juancavallotti/octo/commit/0fbb9c222fa61b23203decb01e4cc77ba3f9a077))
* **orchestrator:** integrations + folders datamodel and layered CRUD API ([fd1e864](https://github.com/juancavallotti/octo/commit/fd1e864a1136b725395bfc51a85cdddc23687cd5))
* **orchestrator:** internal endpoints — replicas + stable per-integration Service ([6c10924](https://github.com/juancavallotti/octo/commit/6c10924e5dd16b8748fa85cb85abc6e5afd2c440))
* **orchestrator:** reject duplicate slugs/subdomains across integrations ([f22b7fc](https://github.com/juancavallotti/octo/commit/f22b7fca19bb44960beb3b0800380236c1edaeed))
* **orchestrator:** scale an existing deployment ([e9c0084](https://github.com/juancavallotti/octo/commit/e9c00847937cb8e300dfe840bba1db888864b03e))
* **orchestrator:** single-folder membership schema + db reset task ([c14396b](https://github.com/juancavallotti/octo/commit/c14396bc3315eae2b92a47d41228d8d31b24927a))
* **orchestrator:** unique per-deployment slugs + user-chosen, validated addresses ([986ee8b](https://github.com/juancavallotti/octo/commit/986ee8b5acfac90eb5ac8cfc2857251e14dca438))
* **orchestrator:** watch the cluster via informers and push status over SSE ([02d7ec7](https://github.com/juancavallotti/octo/commit/02d7ec7f39b53fc614da7649aff952bcbe227137))
* **runtime:** standalone octo-runtime image for per-integration pods ([3f23849](https://github.com/juancavallotti/octo/commit/3f23849f41b02c6d23dfab0c43b57c45d84dec8f))


### Bug Fixes

* **deploy:** grant orchestrator patch on deployments so scaling works ([3e78ec6](https://github.com/juancavallotti/octo/commit/3e78ec6f26691e5e18db07f6a159ac06940c485e))
* **editor:** allow saving any state and keep loaded integrations valid ([ff80de6](https://github.com/juancavallotti/octo/commit/ff80de6872c572aba9b98c8313c341b8fd76bcbe))
* **editor:** gate Save on empty/unchanged, not on a missing name ([1065e86](https://github.com/juancavallotti/octo/commit/1065e86f7394678e91157c74c4b3f1d863917bde))
* **editor:** raise folder picker popover above canvas launchers ([57d5425](https://github.com/juancavallotti/octo/commit/57d542554e2c7f9cd25eca57ad2ba19e33d192f4))


### Refactoring

* **orchestrator:** extract pool lifecycle into internal/db ([8189b5f](https://github.com/juancavallotti/octo/commit/8189b5f23e6d25bcd5f2c67dcbe08399955f51cc))


### Documentation

* **deploy:** document the GCP deployment process ([1b62c59](https://github.com/juancavallotti/octo/commit/1b62c594b3f1a81bb327ca8d5652b324f347e9fa))

## [0.1.1](https://github.com/juancavallotti/octo/compare/v0.1.0...v0.1.1) (2026-06-19)


### Features

* **cli:** add --version flag with build date, standardize doc flags ([c2986d9](https://github.com/juancavallotti/octo/commit/c2986d9db3cb40d3558050e3ada8e7155346c6a3))
* **cli:** add a top-level --help page ([8bf2e2a](https://github.com/juancavallotti/octo/commit/8bf2e2a12f7160cc8402b1d73cc9473d817383e6))
* **editor:** add block settings + rename state actions ([23ee8f3](https://github.com/juancavallotti/octo/commit/23ee8f34714ae6e31560cdf839be32161d45bdb9))
* **editor:** add component settings panel ([4f2cb0d](https://github.com/juancavallotti/octo/commit/4f2cb0d29a13bd41f84906c633838a10d1246191))
* **editor:** add connections manager with referential integrity ([c6908c6](https://github.com/juancavallotti/octo/commit/c6908c649769f355576cae1761436416ed665efa))
* **editor:** add in-memory flow document model and reducer ([402b6ff](https://github.com/juancavallotti/octo/commit/402b6ff24fab98917a04c7070205c3b5ce095a66))
* **editor:** add runtime capability schema ([402ac09](https://github.com/juancavallotti/octo/commit/402ac0919edc3ee6c3268988a7474aedd0a8afe3))
* **editor:** add shared drag-and-drop context ([313e5f1](https://github.com/juancavallotti/octo/commit/313e5f1beebd603433d1fb2b902fbd18585e377e))
* **editor:** add source picker dropdown ([f20241c](https://github.com/juancavallotti/octo/commit/f20241c5a9b5f8cad3f3d0dd8f6cb643af6c93f1))
* **editor:** add source schema accessors and icons ([9366fec](https://github.com/juancavallotti/octo/commit/9366feca98686a493bc9ad2d4960fff5eaa4788f))
* **editor:** add source state (configure, select, edit, remove) ([38d17da](https://github.com/juancavallotti/octo/commit/38d17daed38ceb0630646833377113acbc4b4dff))
* **editor:** add string-list and string-map setting editors ([d08b21d](https://github.com/juancavallotti/octo/commit/d08b21d1073722f556f2a5f0195bb831841a7260))
* **editor:** allow deleting flows ([55146de](https://github.com/juancavallotti/octo/commit/55146def083c5e11e0deae1393e235ab78cc7e67))
* **editor:** allow env vars in typed settings via a field toggle ([fd8321e](https://github.com/juancavallotti/octo/commit/fd8321e09d64d5853c7c0b4159cafa8abe0d5afe))
* **editor:** author environment variables ([1d0a7ba](https://github.com/juancavallotti/octo/commit/1d0a7baf3097545152512e285cb97115bc84b9e1))
* **editor:** bootstrap Octo Next.js visual editor module ([101b8fa](https://github.com/juancavallotti/octo/commit/101b8fab0584d47642c22a0696800d01d7891f32))
* **editor:** bootstrap Octo Next.js visual editor module ([33b9b81](https://github.com/juancavallotti/octo/commit/33b9b81f8fd0430f54c480048a82858fdc0d4786))
* **editor:** drag preview overlay ([b5e556e](https://github.com/juancavallotti/octo/commit/b5e556e1c62a68f4f8e2ba14cd5ff7a25305112a))
* **editor:** edit flow name in settings panel ([5c4578a](https://github.com/juancavallotti/octo/commit/5c4578af54773b156a6eae289eeb6c4791fabc86))
* **editor:** edit nested flows in the reducer ([8311445](https://github.com/juancavallotti/octo/commit/8311445abda5b2d6a45918d373f917e288917f7e))
* **editor:** empty start and opt-in source ([d7eb096](https://github.com/juancavallotti/octo/commit/d7eb0969edeef56b276b270e05716a0fe1315e1b))
* **editor:** gate live config sync on validation, lengthen debounce ([a44be19](https://github.com/juancavallotti/octo/commit/a44be19dae5516c225b8e77618c006542cf16bce))
* **editor:** insertion drop targets ([36a7733](https://github.com/juancavallotti/octo/commit/36a7733cf65c6d2d52f5e16b1732fc40c2253ba6))
* **editor:** make switch cases editable from the properties panel ([f6e0899](https://github.com/juancavallotti/octo/commit/f6e08998e214265e57cfe61f80ae9cd602a8f5e7))
* **editor:** multi-flow stacked canvas with schema-driven palette ([0dcfcb1](https://github.com/juancavallotti/octo/commit/0dcfcb1cb6c0b7561955760d8037f98abebf6c4f))
* **editor:** nested composites with drop-in scopes ([02ee38f](https://github.com/juancavallotti/octo/commit/02ee38fed1e418f7d7b8952e38e3d14fb295e679))
* **editor:** recursive composite-slot model ([eb89005](https://github.com/juancavallotti/octo/commit/eb89005839440244200f62f088901828e818af4e))
* **editor:** render connector/flow reference fields as dropdowns ([81618a2](https://github.com/juancavallotti/octo/commit/81618a2296d66b945caf1b487a06bd2588f9210b))
* **editor:** RUN button and bottom log panel ([b1a7ba4](https://github.com/juancavallotti/octo/commit/b1a7ba43fd96aec23ebd56a387733738fc59560a))
* **editor:** run session API with SSE log streaming ([eb5ed51](https://github.com/juancavallotti/octo/commit/eb5ed516e71c7873eafd9a2392fdbacf70ea31d8))
* **editor:** runnable-config rendering and validity gate ([b910411](https://github.com/juancavallotti/octo/commit/b910411fffc9603d71746737c313f4384cf1d7f4))
* **editor:** schema-driven recursive flow canvas ([a5d6b06](https://github.com/juancavallotti/octo/commit/a5d6b060ae221da63c467d538c89c5d0ad5734bb))
* **editor:** schematic node visuals ([ca3326c](https://github.com/juancavallotti/octo/commit/ca3326c122350e1babb393f9741cdd16d15798d6))
* **editor:** show runtime version in the log panel header ([3c96b53](https://github.com/juancavallotti/octo/commit/3c96b53c15f8d0b3b25192237bd5bc220a1c09e1))
* **editor:** source connector binding and slug flow names ([3beab74](https://github.com/juancavallotti/octo/commit/3beab742030107127b695b691b1fdd603e59f56a))
* **editor:** source settings panel and selectable source node ([8706bf3](https://github.com/juancavallotti/octo/commit/8706bf3f759ef2a17124c39f7fddd1cd6d4e831a))
* **runtime:** start a default connector for sources with no explicit binding ([f45f9e3](https://github.com/juancavallotti/octo/commit/f45f9e36161ef4858d62068d576ca85b96778123))


### Bug Fixes

* **cli:** keep watch mode alive when a config fails to build or start ([80d4448](https://github.com/juancavallotti/octo/commit/80d44481a29d5ae751e01c9b040faf24dba3c194))
* **editor:** constrain editor to viewport so canvas scrolls internally ([ab8948a](https://github.com/juancavallotti/octo/commit/ab8948a3d92b83cd9e3ee614b983e813ddaac2b3))
* **editor:** make a source's connector binding optional for 0-1 connectors ([1593cc6](https://github.com/juancavallotti/octo/commit/1593cc6bb3713102397651ebb44f09a1a0890d5c))
* **editor:** make the clear-logs button actually clear while running ([8e27502](https://github.com/juancavallotti/octo/commit/8e27502dec0d9ba746d4747cef1e32c31e395084))
* **editor:** require a configured connector for flow sources ([06a7e9d](https://github.com/juancavallotti/octo/commit/06a7e9ddb0d07227badc76a1d139a7764c9b2fcc))
* **editor:** resolve hydration warning and logo aspect-ratio warning ([1cf65f3](https://github.com/juancavallotti/octo/commit/1cf65f3d23fd0bd5e1d8ed5b95ddb72ead1ad3b5))


### Documentation

* add editor coding standards and register the editor module ([6053981](https://github.com/juancavallotti/octo/commit/6053981cf5099c27d6dea2aa805dab480136fe8a))

## 0.1.0 (2026-06-15)


### Features

* **cli:** add runtime bootstrap command ([5c0a6d7](https://github.com/juancavallotti/octo/commit/5c0a6d73b6d942b0c32bdaac153ebbb72716bff2))
* **cli:** announce a ready banner with the version on boot ([aa42a4a](https://github.com/juancavallotti/octo/commit/aa42a4adaa22cd785b086a4a567dbe702b297e0b))
* **cli:** hot reload, folder configs, direct flow invocation, and flow-ref block ([1fc9e02](https://github.com/juancavallotti/octo/commit/1fc9e026200d436e9b58add687e00f6676f685a5))
* **cli:** hot reload, folder configs, direct flow invocation, and flow-ref block ([877b995](https://github.com/juancavallotti/octo/commit/877b995fac5bbc3ed3e7167ddb5ea7093a5d1056))
* **cli:** standardize runtime logging with slog ([a3fc373](https://github.com/juancavallotti/octo/commit/a3fc373fc8d0bb5e23187b86e7b5186693cfcdf8))
* **cli:** standardize runtime logging with slog ([babf7ca](https://github.com/juancavallotti/octo/commit/babf7ca898c552bdb098746acd81bcd78c08f6f7))
* **config:** environment variable support with declared vars and .env files ([2155c34](https://github.com/juancavallotti/octo/commit/2155c341b8c7b860b9667e2109cc1e9e203fc650))
* **config:** environment variable support with declared vars and .env files ([0b9fc50](https://github.com/juancavallotti/octo/commit/0b9fc50c492c55252f17eaff59c8786f92171fc9))
* **connectors:** add cron source with CEL payload ([99f9370](https://github.com/juancavallotti/octo/commit/99f937043e06a327b714623049e99826e65cddb8))
* **connectors:** add HTTP connector with request/response sources ([cbde39e](https://github.com/juancavallotti/octo/commit/cbde39e92b9fbf1401f0035bea326ef64173bd6b))
* **connectors:** add logger connector ([8b0193f](https://github.com/juancavallotti/octo/commit/8b0193f7f018cdc96fa9dcc0c94c49e03c3e9fc6))
* **connectors:** add noop self-registering connector ([1a344f4](https://github.com/juancavallotti/octo/commit/1a344f4d70712946cb4656f6d0ff91f38707a832))
* **connectors:** database connector (postgres/sqlite) with a sql block ([8767ff3](https://github.com/juancavallotti/octo/commit/8767ff3a194a552af7739cb783522ec06b00a894))
* **connectors:** database connector with postgres/sqlite and a sql block ([bc016a9](https://github.com/juancavallotti/octo/commit/bc016a91d0c8f2f79aaabcfb295fa4cc892e016d))
* **connectors:** http client connector with a rest block, co-locate blocks ([3c658ca](https://github.com/juancavallotti/octo/commit/3c658ca0e6ca0fb03593afb5dc333afdf5813e02))
* **connectors:** HTTP client connector with a rest block, co-locate blocks ([f683177](https://github.com/juancavallotti/octo/commit/f68317786cc5c4c473a08e96cb17d2fd5ada4c7c))
* **connectors:** HTTP connector with request/response sources ([7e9949a](https://github.com/juancavallotti/octo/commit/7e9949a0e61d6a02e09eb5e945fee0f9b3288e81))
* **connectors:** make noop a source provider ([b6cdd82](https://github.com/juancavallotti/octo/commit/b6cdd82c8cd9871718beb1a53688cd1e2374c44f))
* **core:** add built-in processors and restructure runtime packages ([b783a19](https://github.com/juancavallotti/octo/commit/b783a1975947f3a488ca644ddb0de00610a463c0))
* **core:** add CEL expression engine and named-processor ref resolution ([a759a96](https://github.com/juancavallotti/octo/commit/a759a96424e90880c8393eda2195af4c2537eb8b))
* **core:** add flow composition with scope and fork blocks ([80c482d](https://github.com/juancavallotti/octo/commit/80c482dce71ba51026c01ce5b5b858c015c97f48))
* **core:** add flow-event pub/sub bus ([82e9b51](https://github.com/juancavallotti/octo/commit/82e9b5104776a46eef4688790124ca29f4f50c35))
* **core:** add message processor and block abstractions ([04b86b6](https://github.com/juancavallotti/octo/commit/04b86b60d78e4ba08e417cdf1c93797d950bd6e8))
* **core:** add message source contract and source provider ([cd657bf](https://github.com/juancavallotti/octo/commit/cd657bf1fd86cf81235bd766a7ab62b6a4b704c4))
* **core:** add per-flow worker pool execution ([87106d3](https://github.com/juancavallotti/octo/commit/87106d379a4649e3e0a75c962354979f68bf34a0))
* **core:** add registry and runtime service ([57f9adb](https://github.com/juancavallotti/octo/commit/57f9adb9687409fa1a716df382800c8383f965e1))
* **core:** add registry for built-in leaf blocks ([41127a4](https://github.com/juancavallotti/octo/commit/41127a471d116ddfb5954c2b2ef18c2edb5c9c91))
* **core:** build and run flows in the service lifecycle ([8eda96f](https://github.com/juancavallotti/octo/commit/8eda96fd8796196117b5807dac4ab3389feaa888))
* **core:** built-in processors and runtime package restructure ([3fb8d42](https://github.com/juancavallotti/octo/commit/3fb8d42807ecaff300d14237f3c23d8249437f1f))
* **core:** hybrid execution model with a shared flow pool and concurrent fork ([83f9fc1](https://github.com/juancavallotti/octo/commit/83f9fc1dbe5289184d0785dd7be11b259b7d991f))
* **core:** let blocks resolve connectors, add shared level parsing ([d6c2f2d](https://github.com/juancavallotti/octo/commit/d6c2f2df6b26ef1056f2597854d17684ef777e92))
* logging & cron processors with CEL expressions and named configs ([1c12a3c](https://github.com/juancavallotti/octo/commit/1c12a3cb491039f9d5893b9e21ab61c682be1173))
* processing pipeline runtime with hybrid SEDA/single-threaded execution ([0607e36](https://github.com/juancavallotti/octo/commit/0607e36a133d9455cab37cc7e2d3ffe1115f261c))
* **processors:** add log processor module ([1e1638a](https://github.com/juancavallotti/octo/commit/1e1638a8054851b29db3c9f6b53661bbd147c350))
* **processors:** bind the log block to a logger ([d2a5b16](https://github.com/juancavallotti/octo/commit/d2a5b1609cd4c57d6f4a9f5652af765cf8ad9e5b))
* **tooling:** add interactive new-connector task ([ca4fb70](https://github.com/juancavallotti/octo/commit/ca4fb7068495c6e011a37b49b023748cb0466a6e))
* **tooling:** add interactive new-connector task ([fd70239](https://github.com/juancavallotti/octo/commit/fd70239fedd74fee56533e5ad00d9de5b0c28ed5))
* **types:** add first-class Message and Variables types ([3933811](https://github.com/juancavallotti/octo/commit/39338118609918651741b68e578d1310c44aef86))
* **types:** add flow lifecycle event types ([8df1388](https://github.com/juancavallotti/octo/commit/8df1388f00a8720385da8b033885fc9607882a5a))
* **types:** add Message.Clone for concurrent fork branches ([7f2a1e9](https://github.com/juancavallotti/octo/commit/7f2a1e9854573d82965d50ccd5c2e5f7d06f9a17))
* **types:** add recursive flow, source, and block config ([1b81af7](https://github.com/juancavallotti/octo/commit/1b81af7c32ddeaf813e6cfdc1843ea9bdde76733))
* **types:** add Settings type, named processor configs, and block ref ([d303a2d](https://github.com/juancavallotti/octo/commit/d303a2d8e5a87d9c18283cf9bb8753ff18323e4f))


### Bug Fixes

* **cli:** add replace for transitive types module and commit go.sum ([3eaee89](https://github.com/juancavallotti/octo/commit/3eaee89306a7032a68110c08262ae2cd0eed92a5))
* **lint:** resolve golangci-lint failures in CI validate ([09a7656](https://github.com/juancavallotti/octo/commit/09a7656fb58fe328b025f9caba12de21f904759d))
* **lint:** satisfy golangci-lint in cli and config ([6ff5a43](https://github.com/juancavallotti/octo/commit/6ff5a43ec36d23ea0dc19c23b69e4c288f19dfda))
* **lint:** suppress ireturn on mustBuild test helper ([b4301bb](https://github.com/juancavallotti/octo/commit/b4301bbb4788d116af285b6ec094ae7443b05722))


### Documentation

* allow atomic autonomous commits, gate only on push ([5e249f7](https://github.com/juancavallotti/octo/commit/5e249f7902343c52ab2f4c419f7fe30fdf0ee29e))
* document the processing pipeline building blocks ([91db86d](https://github.com/juancavallotti/octo/commit/91db86d18eabb005bbb3d3ca760f5c80ec0771ce))
* expand Go coding standards and commit/review policy ([2d86ca0](https://github.com/juancavallotti/octo/commit/2d86ca05f9fd923036440ea288ac63e5b15ecb30))
* finalize the composite execution model and refactoring policy ([457da1e](https://github.com/juancavallotti/octo/commit/457da1e40c99cef9747d467ba5508624d3161fd0))
* GitHub Pages site, ready banner, and release-please version sync ([1dc1619](https://github.com/juancavallotti/octo/commit/1dc1619ee2df248d08402ddc28942ad38739711f))
* **repo:** add governance and automation baseline ([ab1ba8c](https://github.com/juancavallotti/octo/commit/ab1ba8c64b5cd3125315532d10ca2cdc71507721))
* **samples:** add flow-to-flow HTTP sample ([5390f01](https://github.com/juancavallotti/octo/commit/5390f01a188af3aabfc7e2bf9eed29257e5b3fe7))
* **site:** add GitHub Pages landing page with diagrams and samples ([4a7d7e5](https://github.com/juancavallotti/octo/commit/4a7d7e5e3f4d132463f4bb4251d15a0c9722c8fd))
