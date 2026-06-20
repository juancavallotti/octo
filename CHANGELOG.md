# Changelog

## [0.1.3](https://github.com/juancavallotti/eip-go/compare/v0.1.2...v0.1.3) (2026-06-20)


### Features

* **deploy:** wildcard TLS cert via DNS-01 for integration subdomains ([#37](https://github.com/juancavallotti/eip-go/issues/37)) ([df99674](https://github.com/juancavallotti/eip-go/commit/df9967417edbdfedcdfbf4086d433fae6cd9778d))
* **editor:** allocate a port and inject HTTP_PORT for networked runs ([#36](https://github.com/juancavallotti/eip-go/issues/36) 2/5) ([86627f2](https://github.com/juancavallotti/eip-go/commit/86627f2ee9fce14dacdeaff4fab11bf7b7c418d1))
* **editor:** namespace editor runs per user ([#36](https://github.com/juancavallotti/eip-go/issues/36) 1/5) ([443247e](https://github.com/juancavallotti/eip-go/commit/443247eba505573a011ce7479b09a13712bba209))
* **editor:** reap idle namespaced runs after 1h ([#36](https://github.com/juancavallotti/eip-go/issues/36) 5/5) ([114c4da](https://github.com/juancavallotti/eip-go/commit/114c4da282f4043b8f40dff6693179f0e45927ea))
* **editor:** reverse-proxy networked runs at /editor/runs/&lt;ns&gt;/ ([#36](https://github.com/juancavallotti/eip-go/issues/36) 3/5) ([82e2f16](https://github.com/juancavallotti/eip-go/commit/82e2f163553ef29ee0053de7f8c076bc0a440b20))
* **editor:** surface the run test URL in the log panel ([#36](https://github.com/juancavallotti/eip-go/issues/36) 4/5) ([7477b89](https://github.com/juancavallotti/eip-go/commit/7477b899bdaa9d27d65a05b70209af8bfc7f044d))
* **runtime:** expose declared env vars to CEL as env.NAME ([#34](https://github.com/juancavallotti/eip-go/issues/34)) ([8e7e48c](https://github.com/juancavallotti/eip-go/commit/8e7e48cc988e29eebe2b2813116632999701d08b))


### Bug Fixes

* **helm:** grant orchestrator RBAC to manage secrets ([#33](https://github.com/juancavallotti/eip-go/issues/33)) ([72e60de](https://github.com/juancavallotti/eip-go/commit/72e60de8ea01a5dd1f86cf52419ebe4c180718aa))
* **http:** release listener on Stop to fix hot-reload port leak ([#22](https://github.com/juancavallotti/eip-go/issues/22)) ([7c66b56](https://github.com/juancavallotti/eip-go/commit/7c66b56d93f2f5bf2fb6ae2ff4ac432f8c80d9f4))
* **infra:** order deploy secret IAM grant after the secret exists ([648d3a0](https://github.com/juancavallotti/eip-go/commit/648d3a0c99a264413e93e4322b3edf59a23db7de))


### Refactoring

* **deploy:** collapse to a single octo.tfvars; drop bootstrap root ([6b3febd](https://github.com/juancavallotti/eip-go/commit/6b3febd584e50f3596e00986f8a4273946856f02))
* **deploy:** one combined infra root + Cloud Build-driven releases ([d905733](https://github.com/juancavallotti/eip-go/commit/d9057333845e94bfa036c060fb761bc3802aea00))

## [0.1.2](https://github.com/juancavallotti/eip-go/compare/v0.1.1...v0.1.2) (2026-06-20)


### Features

* **cluster:** add DevSpace dev mode with hot reload and log tailing ([158dcc5](https://github.com/juancavallotti/eip-go/commit/158dcc52d44823fb8b340843205629c33accaecb))
* **cluster:** add local k3d dev cluster via DevSpace ([7d65b9f](https://github.com/juancavallotti/eip-go/commit/7d65b9fced2dad4c9c8bb0c11ba7bd68767eca00))
* **cluster:** local k3d dev cluster via DevSpace (editor+runtime, orchestrator, sql) ([e9cf2a2](https://github.com/juancavallotti/eip-go/commit/e9cf2a2bf36937e030ab929a9082e60c80618a18))
* deploy integrations as Kubernetes pods ([f1d9f7d](https://github.com/juancavallotti/eip-go/commit/f1d9f7d1bded654686b7e3e5a6f8d5f1c9ea0cd3))
* **deploy:** Helm chart, Artifact Registry + Cloud Build for GCP ([b81e9aa](https://github.com/juancavallotti/eip-go/commit/b81e9aa32bd2f3687293271bd0581b45329ac840))
* **deploy:** single-node k3s VM + Terraform-owned Helm release ([bd5f9f0](https://github.com/juancavallotti/eip-go/commit/bd5f9f0681e96e36f5fc07311288f7067984e268))
* **editor:** bookmarkable /i/[id] route for opening integrations ([5e74c70](https://github.com/juancavallotti/eip-go/commit/5e74c7087887f3c83f254e75398acd12aabf3918))
* **editor:** deploy and manage integrations from the management UI ([dc0d992](https://github.com/juancavallotti/eip-go/commit/dc0d992442183f833cbc93a34a6617e67baa5707))
* **editor:** deployment UX — formatting, icons, modal, and scaling ([815700b](https://github.com/juancavallotti/eip-go/commit/815700bd84eb66e43752916fdde2f8040bbd26cc))
* **editor:** editable title, folder picker and save in title bar ([9705b1b](https://github.com/juancavallotti/eip-go/commit/9705b1b3ee16b917f17fc0f407484b8d08ed6ecf))
* **editor:** integrate orchestrator integrations & folders ([6dae196](https://github.com/juancavallotti/eip-go/commit/6dae196c15cc31ccce41c3859eba52495a9682bd))
* **editor:** integrations management route with folder CRUD and detail panel ([a29c13d](https://github.com/juancavallotti/eip-go/commit/a29c13d7eb1346d3f10dc5c075d7bbb5120c4561))
* **editor:** live deployment status over SSE with a polling fallback ([e3afb57](https://github.com/juancavallotti/eip-go/commit/e3afb577ed8fd64d535bb9a7849b9702bc18b642))
* **editor:** orchestrator API client and proxy routes ([94e0504](https://github.com/juancavallotti/eip-go/commit/94e0504f5b102d84f1c32c490b0325548ec8978d))
* **editor:** save the integration with Cmd/Ctrl+S ([4306e07](https://github.com/juancavallotti/eip-go/commit/4306e079794f96d4da6b870ac0e757db11e91558))
* **editor:** user-chosen deployment slug with live validation ([af17454](https://github.com/juancavallotti/eip-go/commit/af1745435c5de8018d679be9cce025342f8e3ae7))
* **orchestrator,editor:** external per-integration subdomains ([6dfb6d0](https://github.com/juancavallotti/eip-go/commit/6dfb6d087cd472675e77918a0c3af199c5b01b73))
* **orchestrator,editor:** richer deployment status ([2451c4f](https://github.com/juancavallotti/eip-go/commit/2451c4ff4e81ed6e65bc2f25701f4b06a4092657))
* **orchestrator:** add integration repository layer ([7f24c0e](https://github.com/juancavallotti/eip-go/commit/7f24c0eed16995f129d609f96068622cf7fea4f1))
* **orchestrator:** add integration REST endpoints ([7991942](https://github.com/juancavallotti/eip-go/commit/7991942c25412bff8d75a550e635aadc2436a21b))
* **orchestrator:** add integration service layer ([8932127](https://github.com/juancavallotti/eip-go/commit/8932127f7b1f59e051f9c3d7d50757011ba5ac76))
* **orchestrator:** add integrations datamodel to schema ([8712255](https://github.com/juancavallotti/eip-go/commit/8712255042ec56ec8d60a7d8b0873b0f93d0dd70))
* **orchestrator:** deploy integrations as Kubernetes workloads ([3eae392](https://github.com/juancavallotti/eip-go/commit/3eae3923dde52d4fb990c89b1521b159936eae18))
* **orchestrator:** folder HTTP API and route wiring ([b5b9544](https://github.com/juancavallotti/eip-go/commit/b5b9544c284371402db0eb54b24448945ce8d713))
* **orchestrator:** folder repo, types and DB tests ([61f333e](https://github.com/juancavallotti/eip-go/commit/61f333e1844b423fb4183fb43a3c00b95b016fd0))
* **orchestrator:** folder service with validation and tree assembly ([d6cec19](https://github.com/juancavallotti/eip-go/commit/d6cec1954970a085521b92c741f8593683a720d9))
* **orchestrator:** HTTP_PORT drives external exposability + port wiring ([0fbb9c2](https://github.com/juancavallotti/eip-go/commit/0fbb9c222fa61b23203decb01e4cc77ba3f9a077))
* **orchestrator:** integrations + folders datamodel and layered CRUD API ([fd1e864](https://github.com/juancavallotti/eip-go/commit/fd1e864a1136b725395bfc51a85cdddc23687cd5))
* **orchestrator:** internal endpoints — replicas + stable per-integration Service ([6c10924](https://github.com/juancavallotti/eip-go/commit/6c10924e5dd16b8748fa85cb85abc6e5afd2c440))
* **orchestrator:** reject duplicate slugs/subdomains across integrations ([f22b7fc](https://github.com/juancavallotti/eip-go/commit/f22b7fca19bb44960beb3b0800380236c1edaeed))
* **orchestrator:** scale an existing deployment ([e9c0084](https://github.com/juancavallotti/eip-go/commit/e9c00847937cb8e300dfe840bba1db888864b03e))
* **orchestrator:** single-folder membership schema + db reset task ([c14396b](https://github.com/juancavallotti/eip-go/commit/c14396bc3315eae2b92a47d41228d8d31b24927a))
* **orchestrator:** unique per-deployment slugs + user-chosen, validated addresses ([986ee8b](https://github.com/juancavallotti/eip-go/commit/986ee8b5acfac90eb5ac8cfc2857251e14dca438))
* **orchestrator:** watch the cluster via informers and push status over SSE ([02d7ec7](https://github.com/juancavallotti/eip-go/commit/02d7ec7f39b53fc614da7649aff952bcbe227137))
* **runtime:** standalone octo-runtime image for per-integration pods ([3f23849](https://github.com/juancavallotti/eip-go/commit/3f23849f41b02c6d23dfab0c43b57c45d84dec8f))


### Bug Fixes

* **deploy:** grant orchestrator patch on deployments so scaling works ([3e78ec6](https://github.com/juancavallotti/eip-go/commit/3e78ec6f26691e5e18db07f6a159ac06940c485e))
* **editor:** allow saving any state and keep loaded integrations valid ([ff80de6](https://github.com/juancavallotti/eip-go/commit/ff80de6872c572aba9b98c8313c341b8fd76bcbe))
* **editor:** gate Save on empty/unchanged, not on a missing name ([1065e86](https://github.com/juancavallotti/eip-go/commit/1065e86f7394678e91157c74c4b3f1d863917bde))
* **editor:** raise folder picker popover above canvas launchers ([57d5425](https://github.com/juancavallotti/eip-go/commit/57d542554e2c7f9cd25eca57ad2ba19e33d192f4))


### Refactoring

* **orchestrator:** extract pool lifecycle into internal/db ([8189b5f](https://github.com/juancavallotti/eip-go/commit/8189b5f23e6d25bcd5f2c67dcbe08399955f51cc))


### Documentation

* **deploy:** document the GCP deployment process ([1b62c59](https://github.com/juancavallotti/eip-go/commit/1b62c594b3f1a81bb327ca8d5652b324f347e9fa))

## [0.1.1](https://github.com/juancavallotti/eip-go/compare/v0.1.0...v0.1.1) (2026-06-19)


### Features

* **cli:** add --version flag with build date, standardize doc flags ([c2986d9](https://github.com/juancavallotti/eip-go/commit/c2986d9db3cb40d3558050e3ada8e7155346c6a3))
* **cli:** add a top-level --help page ([8bf2e2a](https://github.com/juancavallotti/eip-go/commit/8bf2e2a12f7160cc8402b1d73cc9473d817383e6))
* **editor:** add block settings + rename state actions ([23ee8f3](https://github.com/juancavallotti/eip-go/commit/23ee8f34714ae6e31560cdf839be32161d45bdb9))
* **editor:** add component settings panel ([4f2cb0d](https://github.com/juancavallotti/eip-go/commit/4f2cb0d29a13bd41f84906c633838a10d1246191))
* **editor:** add connections manager with referential integrity ([c6908c6](https://github.com/juancavallotti/eip-go/commit/c6908c649769f355576cae1761436416ed665efa))
* **editor:** add in-memory flow document model and reducer ([402b6ff](https://github.com/juancavallotti/eip-go/commit/402b6ff24fab98917a04c7070205c3b5ce095a66))
* **editor:** add runtime capability schema ([402ac09](https://github.com/juancavallotti/eip-go/commit/402ac0919edc3ee6c3268988a7474aedd0a8afe3))
* **editor:** add shared drag-and-drop context ([313e5f1](https://github.com/juancavallotti/eip-go/commit/313e5f1beebd603433d1fb2b902fbd18585e377e))
* **editor:** add source picker dropdown ([f20241c](https://github.com/juancavallotti/eip-go/commit/f20241c5a9b5f8cad3f3d0dd8f6cb643af6c93f1))
* **editor:** add source schema accessors and icons ([9366fec](https://github.com/juancavallotti/eip-go/commit/9366feca98686a493bc9ad2d4960fff5eaa4788f))
* **editor:** add source state (configure, select, edit, remove) ([38d17da](https://github.com/juancavallotti/eip-go/commit/38d17daed38ceb0630646833377113acbc4b4dff))
* **editor:** add string-list and string-map setting editors ([d08b21d](https://github.com/juancavallotti/eip-go/commit/d08b21d1073722f556f2a5f0195bb831841a7260))
* **editor:** allow deleting flows ([55146de](https://github.com/juancavallotti/eip-go/commit/55146def083c5e11e0deae1393e235ab78cc7e67))
* **editor:** allow env vars in typed settings via a field toggle ([fd8321e](https://github.com/juancavallotti/eip-go/commit/fd8321e09d64d5853c7c0b4159cafa8abe0d5afe))
* **editor:** author environment variables ([1d0a7ba](https://github.com/juancavallotti/eip-go/commit/1d0a7baf3097545152512e285cb97115bc84b9e1))
* **editor:** bootstrap Octo Next.js visual editor module ([101b8fa](https://github.com/juancavallotti/eip-go/commit/101b8fab0584d47642c22a0696800d01d7891f32))
* **editor:** bootstrap Octo Next.js visual editor module ([33b9b81](https://github.com/juancavallotti/eip-go/commit/33b9b81f8fd0430f54c480048a82858fdc0d4786))
* **editor:** drag preview overlay ([b5e556e](https://github.com/juancavallotti/eip-go/commit/b5e556e1c62a68f4f8e2ba14cd5ff7a25305112a))
* **editor:** edit flow name in settings panel ([5c4578a](https://github.com/juancavallotti/eip-go/commit/5c4578af54773b156a6eae289eeb6c4791fabc86))
* **editor:** edit nested flows in the reducer ([8311445](https://github.com/juancavallotti/eip-go/commit/8311445abda5b2d6a45918d373f917e288917f7e))
* **editor:** empty start and opt-in source ([d7eb096](https://github.com/juancavallotti/eip-go/commit/d7eb0969edeef56b276b270e05716a0fe1315e1b))
* **editor:** gate live config sync on validation, lengthen debounce ([a44be19](https://github.com/juancavallotti/eip-go/commit/a44be19dae5516c225b8e77618c006542cf16bce))
* **editor:** insertion drop targets ([36a7733](https://github.com/juancavallotti/eip-go/commit/36a7733cf65c6d2d52f5e16b1732fc40c2253ba6))
* **editor:** make switch cases editable from the properties panel ([f6e0899](https://github.com/juancavallotti/eip-go/commit/f6e08998e214265e57cfe61f80ae9cd602a8f5e7))
* **editor:** multi-flow stacked canvas with schema-driven palette ([0dcfcb1](https://github.com/juancavallotti/eip-go/commit/0dcfcb1cb6c0b7561955760d8037f98abebf6c4f))
* **editor:** nested composites with drop-in scopes ([02ee38f](https://github.com/juancavallotti/eip-go/commit/02ee38fed1e418f7d7b8952e38e3d14fb295e679))
* **editor:** recursive composite-slot model ([eb89005](https://github.com/juancavallotti/eip-go/commit/eb89005839440244200f62f088901828e818af4e))
* **editor:** render connector/flow reference fields as dropdowns ([81618a2](https://github.com/juancavallotti/eip-go/commit/81618a2296d66b945caf1b487a06bd2588f9210b))
* **editor:** RUN button and bottom log panel ([b1a7ba4](https://github.com/juancavallotti/eip-go/commit/b1a7ba43fd96aec23ebd56a387733738fc59560a))
* **editor:** run session API with SSE log streaming ([eb5ed51](https://github.com/juancavallotti/eip-go/commit/eb5ed516e71c7873eafd9a2392fdbacf70ea31d8))
* **editor:** runnable-config rendering and validity gate ([b910411](https://github.com/juancavallotti/eip-go/commit/b910411fffc9603d71746737c313f4384cf1d7f4))
* **editor:** schema-driven recursive flow canvas ([a5d6b06](https://github.com/juancavallotti/eip-go/commit/a5d6b060ae221da63c467d538c89c5d0ad5734bb))
* **editor:** schematic node visuals ([ca3326c](https://github.com/juancavallotti/eip-go/commit/ca3326c122350e1babb393f9741cdd16d15798d6))
* **editor:** show runtime version in the log panel header ([3c96b53](https://github.com/juancavallotti/eip-go/commit/3c96b53c15f8d0b3b25192237bd5bc220a1c09e1))
* **editor:** source connector binding and slug flow names ([3beab74](https://github.com/juancavallotti/eip-go/commit/3beab742030107127b695b691b1fdd603e59f56a))
* **editor:** source settings panel and selectable source node ([8706bf3](https://github.com/juancavallotti/eip-go/commit/8706bf3f759ef2a17124c39f7fddd1cd6d4e831a))
* **runtime:** start a default connector for sources with no explicit binding ([f45f9e3](https://github.com/juancavallotti/eip-go/commit/f45f9e36161ef4858d62068d576ca85b96778123))


### Bug Fixes

* **cli:** keep watch mode alive when a config fails to build or start ([80d4448](https://github.com/juancavallotti/eip-go/commit/80d44481a29d5ae751e01c9b040faf24dba3c194))
* **editor:** constrain editor to viewport so canvas scrolls internally ([ab8948a](https://github.com/juancavallotti/eip-go/commit/ab8948a3d92b83cd9e3ee614b983e813ddaac2b3))
* **editor:** make a source's connector binding optional for 0-1 connectors ([1593cc6](https://github.com/juancavallotti/eip-go/commit/1593cc6bb3713102397651ebb44f09a1a0890d5c))
* **editor:** make the clear-logs button actually clear while running ([8e27502](https://github.com/juancavallotti/eip-go/commit/8e27502dec0d9ba746d4747cef1e32c31e395084))
* **editor:** require a configured connector for flow sources ([06a7e9d](https://github.com/juancavallotti/eip-go/commit/06a7e9ddb0d07227badc76a1d139a7764c9b2fcc))
* **editor:** resolve hydration warning and logo aspect-ratio warning ([1cf65f3](https://github.com/juancavallotti/eip-go/commit/1cf65f3d23fd0bd5e1d8ed5b95ddb72ead1ad3b5))


### Documentation

* add editor coding standards and register the editor module ([6053981](https://github.com/juancavallotti/eip-go/commit/6053981cf5099c27d6dea2aa805dab480136fe8a))

## 0.1.0 (2026-06-15)


### Features

* **cli:** add runtime bootstrap command ([5c0a6d7](https://github.com/juancavallotti/eip-go/commit/5c0a6d73b6d942b0c32bdaac153ebbb72716bff2))
* **cli:** announce a ready banner with the version on boot ([aa42a4a](https://github.com/juancavallotti/eip-go/commit/aa42a4adaa22cd785b086a4a567dbe702b297e0b))
* **cli:** hot reload, folder configs, direct flow invocation, and flow-ref block ([1fc9e02](https://github.com/juancavallotti/eip-go/commit/1fc9e026200d436e9b58add687e00f6676f685a5))
* **cli:** hot reload, folder configs, direct flow invocation, and flow-ref block ([877b995](https://github.com/juancavallotti/eip-go/commit/877b995fac5bbc3ed3e7167ddb5ea7093a5d1056))
* **cli:** standardize runtime logging with slog ([a3fc373](https://github.com/juancavallotti/eip-go/commit/a3fc373fc8d0bb5e23187b86e7b5186693cfcdf8))
* **cli:** standardize runtime logging with slog ([babf7ca](https://github.com/juancavallotti/eip-go/commit/babf7ca898c552bdb098746acd81bcd78c08f6f7))
* **config:** environment variable support with declared vars and .env files ([2155c34](https://github.com/juancavallotti/eip-go/commit/2155c341b8c7b860b9667e2109cc1e9e203fc650))
* **config:** environment variable support with declared vars and .env files ([0b9fc50](https://github.com/juancavallotti/eip-go/commit/0b9fc50c492c55252f17eaff59c8786f92171fc9))
* **connectors:** add cron source with CEL payload ([99f9370](https://github.com/juancavallotti/eip-go/commit/99f937043e06a327b714623049e99826e65cddb8))
* **connectors:** add HTTP connector with request/response sources ([cbde39e](https://github.com/juancavallotti/eip-go/commit/cbde39e92b9fbf1401f0035bea326ef64173bd6b))
* **connectors:** add logger connector ([8b0193f](https://github.com/juancavallotti/eip-go/commit/8b0193f7f018cdc96fa9dcc0c94c49e03c3e9fc6))
* **connectors:** add noop self-registering connector ([1a344f4](https://github.com/juancavallotti/eip-go/commit/1a344f4d70712946cb4656f6d0ff91f38707a832))
* **connectors:** database connector (postgres/sqlite) with a sql block ([8767ff3](https://github.com/juancavallotti/eip-go/commit/8767ff3a194a552af7739cb783522ec06b00a894))
* **connectors:** database connector with postgres/sqlite and a sql block ([bc016a9](https://github.com/juancavallotti/eip-go/commit/bc016a91d0c8f2f79aaabcfb295fa4cc892e016d))
* **connectors:** http client connector with a rest block, co-locate blocks ([3c658ca](https://github.com/juancavallotti/eip-go/commit/3c658ca0e6ca0fb03593afb5dc333afdf5813e02))
* **connectors:** HTTP client connector with a rest block, co-locate blocks ([f683177](https://github.com/juancavallotti/eip-go/commit/f68317786cc5c4c473a08e96cb17d2fd5ada4c7c))
* **connectors:** HTTP connector with request/response sources ([7e9949a](https://github.com/juancavallotti/eip-go/commit/7e9949a0e61d6a02e09eb5e945fee0f9b3288e81))
* **connectors:** make noop a source provider ([b6cdd82](https://github.com/juancavallotti/eip-go/commit/b6cdd82c8cd9871718beb1a53688cd1e2374c44f))
* **core:** add built-in processors and restructure runtime packages ([b783a19](https://github.com/juancavallotti/eip-go/commit/b783a1975947f3a488ca644ddb0de00610a463c0))
* **core:** add CEL expression engine and named-processor ref resolution ([a759a96](https://github.com/juancavallotti/eip-go/commit/a759a96424e90880c8393eda2195af4c2537eb8b))
* **core:** add flow composition with scope and fork blocks ([80c482d](https://github.com/juancavallotti/eip-go/commit/80c482dce71ba51026c01ce5b5b858c015c97f48))
* **core:** add flow-event pub/sub bus ([82e9b51](https://github.com/juancavallotti/eip-go/commit/82e9b5104776a46eef4688790124ca29f4f50c35))
* **core:** add message processor and block abstractions ([04b86b6](https://github.com/juancavallotti/eip-go/commit/04b86b60d78e4ba08e417cdf1c93797d950bd6e8))
* **core:** add message source contract and source provider ([cd657bf](https://github.com/juancavallotti/eip-go/commit/cd657bf1fd86cf81235bd766a7ab62b6a4b704c4))
* **core:** add per-flow worker pool execution ([87106d3](https://github.com/juancavallotti/eip-go/commit/87106d379a4649e3e0a75c962354979f68bf34a0))
* **core:** add registry and runtime service ([57f9adb](https://github.com/juancavallotti/eip-go/commit/57f9adb9687409fa1a716df382800c8383f965e1))
* **core:** add registry for built-in leaf blocks ([41127a4](https://github.com/juancavallotti/eip-go/commit/41127a471d116ddfb5954c2b2ef18c2edb5c9c91))
* **core:** build and run flows in the service lifecycle ([8eda96f](https://github.com/juancavallotti/eip-go/commit/8eda96fd8796196117b5807dac4ab3389feaa888))
* **core:** built-in processors and runtime package restructure ([3fb8d42](https://github.com/juancavallotti/eip-go/commit/3fb8d42807ecaff300d14237f3c23d8249437f1f))
* **core:** hybrid execution model with a shared flow pool and concurrent fork ([83f9fc1](https://github.com/juancavallotti/eip-go/commit/83f9fc1dbe5289184d0785dd7be11b259b7d991f))
* **core:** let blocks resolve connectors, add shared level parsing ([d6c2f2d](https://github.com/juancavallotti/eip-go/commit/d6c2f2df6b26ef1056f2597854d17684ef777e92))
* logging & cron processors with CEL expressions and named configs ([1c12a3c](https://github.com/juancavallotti/eip-go/commit/1c12a3cb491039f9d5893b9e21ab61c682be1173))
* processing pipeline runtime with hybrid SEDA/single-threaded execution ([0607e36](https://github.com/juancavallotti/eip-go/commit/0607e36a133d9455cab37cc7e2d3ffe1115f261c))
* **processors:** add log processor module ([1e1638a](https://github.com/juancavallotti/eip-go/commit/1e1638a8054851b29db3c9f6b53661bbd147c350))
* **processors:** bind the log block to a logger ([d2a5b16](https://github.com/juancavallotti/eip-go/commit/d2a5b1609cd4c57d6f4a9f5652af765cf8ad9e5b))
* **tooling:** add interactive new-connector task ([ca4fb70](https://github.com/juancavallotti/eip-go/commit/ca4fb7068495c6e011a37b49b023748cb0466a6e))
* **tooling:** add interactive new-connector task ([fd70239](https://github.com/juancavallotti/eip-go/commit/fd70239fedd74fee56533e5ad00d9de5b0c28ed5))
* **types:** add first-class Message and Variables types ([3933811](https://github.com/juancavallotti/eip-go/commit/39338118609918651741b68e578d1310c44aef86))
* **types:** add flow lifecycle event types ([8df1388](https://github.com/juancavallotti/eip-go/commit/8df1388f00a8720385da8b033885fc9607882a5a))
* **types:** add Message.Clone for concurrent fork branches ([7f2a1e9](https://github.com/juancavallotti/eip-go/commit/7f2a1e9854573d82965d50ccd5c2e5f7d06f9a17))
* **types:** add recursive flow, source, and block config ([1b81af7](https://github.com/juancavallotti/eip-go/commit/1b81af7c32ddeaf813e6cfdc1843ea9bdde76733))
* **types:** add Settings type, named processor configs, and block ref ([d303a2d](https://github.com/juancavallotti/eip-go/commit/d303a2d8e5a87d9c18283cf9bb8753ff18323e4f))


### Bug Fixes

* **cli:** add replace for transitive types module and commit go.sum ([3eaee89](https://github.com/juancavallotti/eip-go/commit/3eaee89306a7032a68110c08262ae2cd0eed92a5))
* **lint:** resolve golangci-lint failures in CI validate ([09a7656](https://github.com/juancavallotti/eip-go/commit/09a7656fb58fe328b025f9caba12de21f904759d))
* **lint:** satisfy golangci-lint in cli and config ([6ff5a43](https://github.com/juancavallotti/eip-go/commit/6ff5a43ec36d23ea0dc19c23b69e4c288f19dfda))
* **lint:** suppress ireturn on mustBuild test helper ([b4301bb](https://github.com/juancavallotti/eip-go/commit/b4301bbb4788d116af285b6ec094ae7443b05722))


### Documentation

* allow atomic autonomous commits, gate only on push ([5e249f7](https://github.com/juancavallotti/eip-go/commit/5e249f7902343c52ab2f4c419f7fe30fdf0ee29e))
* document the processing pipeline building blocks ([91db86d](https://github.com/juancavallotti/eip-go/commit/91db86d18eabb005bbb3d3ca760f5c80ec0771ce))
* expand Go coding standards and commit/review policy ([2d86ca0](https://github.com/juancavallotti/eip-go/commit/2d86ca05f9fd923036440ea288ac63e5b15ecb30))
* finalize the composite execution model and refactoring policy ([457da1e](https://github.com/juancavallotti/eip-go/commit/457da1e40c99cef9747d467ba5508624d3161fd0))
* GitHub Pages site, ready banner, and release-please version sync ([1dc1619](https://github.com/juancavallotti/eip-go/commit/1dc1619ee2df248d08402ddc28942ad38739711f))
* **repo:** add governance and automation baseline ([ab1ba8c](https://github.com/juancavallotti/eip-go/commit/ab1ba8c64b5cd3125315532d10ca2cdc71507721))
* **samples:** add flow-to-flow HTTP sample ([5390f01](https://github.com/juancavallotti/eip-go/commit/5390f01a188af3aabfc7e2bf9eed29257e5b3fe7))
* **site:** add GitHub Pages landing page with diagrams and samples ([4a7d7e5](https://github.com/juancavallotti/eip-go/commit/4a7d7e5e3f4d132463f4bb4251d15a0c9722c8fd))
