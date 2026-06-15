# Changelog

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
