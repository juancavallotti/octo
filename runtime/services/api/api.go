// Package api implements the runtime services provider that delegates every
// platform capability — KV, secrets, resources, leases, leader election, queues,
// topics, agent memory, traces and logs — to an HTTP API somebody else
// implements.
//
// It is a consumer-defined interface: this repo defines the contract and
// publishes it as an OpenAPI document, and the operator implements it against
// whatever their platform already has. On Cloud Run that is usually Firestore,
// Secret Manager and Pub/Sub behind a second service; on Kubernetes it is a
// central service the runtime pods call, or a sidecar container on loopback.
// Whatever is behind it, the runtime sees one URL.
//
// The contract is negotiated rather than assumed. At startup the module fetches a
// discovery document naming which features the server implements and how each is
// configured; anything unimplemented resolves — once, at construction — to core's
// no-op for that capability or to an implementation that refuses. So no code path
// below this file asks whether the thing it is calling exists.
//
// It self-registers as the "api" module; a binary blank-imports it to make it
// selectable via RUNTIME_SERVICES_MODULE=api.
package api

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/services"
)

// Module is this provider's name, matched against RUNTIME_SERVICES_MODULE.
const Module = "api"

func init() { registerProvider() }

func registerProvider() { services.Register(Module, New) }

// Services is the API-delegating runtime-services provider.
type Services struct {
	client *client
	cfg    Config
	doc    discoveryDocument

	// cancel stops every background poll loop this module runs — the queue and
	// topic receivers, the lease renewals, the leader campaigns. It is the one
	// structural difference from the other two modules, whose background work
	// belongs to a library rather than to us.
	cancel context.CancelFunc

	le        core.LeaderElection
	leases    core.Leases
	kv        core.KV
	secrets   core.SecretStore
	queues    core.Queues
	topics    core.Topics
	resources core.ResourceLoader
	memory    core.AgentMemory
	traces    core.TracePublisher
}

// New builds the provider: it reads the environment, negotiates the contract, and
// resolves every capability to a concrete implementation.
//
// Unlike the k8s module it uses its context, because discovery is a network call
// that can outlive a caller's patience and the CLI's shutdown signal has to be
// able to reach it.
//
//nolint:ireturn // satisfies services.Factory (returns core.RuntimeServices)
func New(ctx context.Context, opts services.Options) (core.RuntimeServices, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	c, err := newClient(cfg)
	if err != nil {
		return nil, err
	}

	doc, err := fetchDiscovery(ctx, c, cfg)
	if err != nil {
		if cfg.Startup == StartupRequire {
			c.close()
			return nil, fmt.Errorf("%w (set %s=%s to start with every capability degraded instead)",
				err, envStartup, StartupDegrade)
		}
		slog.Error("api: starting with every platform capability unavailable",
			"url", cfg.BaseURL, "error", err, "policy", cfg.Startup)
		doc = discoveryDocument{SpecVersion: specVersion}
	}
	warnSpecSkew(doc, cfg)

	// The run context is detached from the caller's: New's context bounds startup,
	// whereas the poll loops must live until Close.
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	svc := &Services{client: c, cfg: cfg, doc: doc, cancel: cancel}
	svc.build(runCtx, opts)

	slog.Info("api runtime services initialized",
		"url", cfg.BaseURL, "instance", cfg.InstanceID, "deployment", cfg.DeploymentID,
		"implementation", doc.Implementation.Name, "implementationVersion", doc.Implementation.Version,
		"features", supportedFeatures(doc))
	return svc, nil
}

// build resolves each capability. Every field is assigned exactly once here, which
// is what lets the rest of the module treat its dependencies as unconditionally
// present.
//
// It is populated capability by capability as each is implemented; until then a
// capability resolves to its unsupported form.
func (s *Services) build(_ context.Context, _ services.Options) {
	s.kv = buildKV(s.client, s.doc.Features.KV)
	s.secrets = buildSecrets(s.kv, s.doc.Features.Secrets, s.doc.Features.KV)
	s.resources = buildResources(s.client, s.doc.Features.Resources)
	s.leases = degradedLeases(s.doc.Features.Leases.featureFlags)
	s.le = degradedLeaderElection(s.doc.Features.LeaderElection.featureFlags)
	s.queues = degradedQueues(s.doc.Features.Queues.featureFlags)
	s.topics = degradedTopics(s.doc.Features.Topics.featureFlags)
	s.memory = core.NoopAgentMemory()
	s.traces = core.NoopTracer()
}

// buildKV resolves the key-value store.
//
//nolint:ireturn // resolves to core.KV: the store, the no-op, or the refusal
func buildKV(c *client, f kvFeature) core.KV {
	if f.Supported {
		return newKVStore(c, f)
	}
	return degradedKV(f.featureFlags)
}

// degradedKV returns the unsupported form of the KV store.
//
//nolint:ireturn // resolves to core.KV
func degradedKV(f featureFlags) core.KV {
	if policyFor(FeatureKV, f.Unsupported) == PolicyError {
		return erroringKV{}
	}
	return core.NoopKV()
}

// buildSecrets resolves the secret store.
//
// It is a view over KV rather than a store of its own — core.NewSecretStore maps
// each namespace to its encrypted counterpart, which is how all three modules do
// it — so the only decision here is whether to allow that view at all.
//
//nolint:ireturn // resolves to core.SecretStore
func buildSecrets(kv core.KV, f secretsFeature, kvf kvFeature) core.SecretStore {
	if !f.Supported && policyFor(FeatureSecrets, f.Unsupported) == PolicyError {
		return erroringSecrets{}
	}
	if f.Supported && !f.EncryptedAtRest {
		slog.Warn("api: the platform API does not claim to encrypt secrets at rest; "+
			"values written to the secret namespaces may be stored in the clear",
			"namespaces", []string{core.NamespaceSystemSecrets, core.NamespaceUserSecrets})
	}
	if !kvf.Supported {
		slog.Warn("api: the platform API implements no key-value store, so secrets have " +
			"nowhere to live either")
	}
	return core.NewSecretStore(kv)
}

// buildResources resolves the resource loader.
//
//nolint:ireturn // resolves to core.ResourceLoader
func buildResources(c *client, f featureFlags) core.ResourceLoader {
	if f.Supported {
		return newResourceLoader(c)
	}
	return degradedResources(f)
}

//nolint:ireturn // resolves to core.ResourceLoader
func degradedResources(f featureFlags) core.ResourceLoader {
	if policyFor(FeatureResources, f.Unsupported) == PolicyError {
		return erroringResources{}
	}
	return core.NoopResourceLoader{}
}

//nolint:ireturn // resolves to core.Leases
func degradedLeases(f featureFlags) core.Leases {
	if policyFor(FeatureLeases, f.Unsupported) == PolicyError {
		return erroringLeases{}
	}
	return core.NoopLeases()
}

//nolint:ireturn // resolves to core.LeaderElection
func degradedLeaderElection(f featureFlags) core.LeaderElection {
	if policyFor(FeatureLeaderElection, f.Unsupported) == PolicyError {
		return erroringLeaderElection{}
	}
	return core.NoopLeaderElection()
}

//nolint:ireturn // resolves to core.Queues
func degradedQueues(f featureFlags) core.Queues {
	if policyFor(FeatureQueues, f.Unsupported) == PolicyError {
		return erroringQueues{}
	}
	return core.NoopQueues()
}

//nolint:ireturn // resolves to core.Topics
func degradedTopics(f featureFlags) core.Topics {
	if policyFor(FeatureTopics, f.Unsupported) == PolicyError {
		return erroringTopics{}
	}
	return core.NoopTopics()
}

// warnSpecSkew reports a server speaking a different contract version. It warns
// and continues: refusing to start over a version string turns a working
// deployment into an outage over a typo, and every field in this contract is
// already optional.
func warnSpecSkew(doc discoveryDocument, cfg Config) {
	if doc.SpecVersion == "" {
		slog.Warn("api: the platform API declared no specVersion",
			"url", cfg.BaseURL, "expected", specVersion)
		return
	}
	if doc.SpecVersion != specVersion {
		slog.Warn("api: the platform API speaks a different contract version",
			"url", cfg.BaseURL, "declared", doc.SpecVersion, "expected", specVersion)
	}
}

// supportedFeatures lists what the server said it implements, for the startup log.
func supportedFeatures(doc discoveryDocument) []string {
	f := doc.Features
	declared := map[Feature]bool{
		FeatureKV:             f.KV.Supported,
		FeatureSecrets:        f.Secrets.Supported,
		FeatureResources:      f.Resources.Supported,
		FeatureLeases:         f.Leases.Supported,
		FeatureLeaderElection: f.LeaderElection.Supported,
		FeatureQueues:         f.Queues.Supported,
		FeatureTopics:         f.Topics.Supported,
		FeatureAgentMemory:    f.AgentMemory.Supported,
		FeatureTraces:         f.Traces.Supported,
		FeatureLogs:           f.Logs.Supported,
	}
	var out []string
	for _, name := range featureOrder {
		if declared[name] {
			out = append(out, string(name))
		}
	}
	return out
}

// featureOrder fixes the order features are logged in, so two startup lines from
// different runs can be compared by eye.
var featureOrder = []Feature{
	FeatureKV, FeatureSecrets, FeatureResources, FeatureLeases, FeatureLeaderElection,
	FeatureQueues, FeatureTopics, FeatureAgentMemory, FeatureTraces, FeatureLogs,
}

// LeaderElection returns the campaign-based leader election.
//
//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) LeaderElection() core.LeaderElection { return s.le }

// Leases returns the fail-fast claims.
//
//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) Leases() core.Leases { return s.leases }

// KV returns the key/value store.
//
//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) KV() core.KV { return s.kv }

// Secrets returns the store's view of the encrypted namespaces.
//
//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) Secrets() core.SecretStore { return s.secrets }

// Queues returns the point-to-point message queues.
//
//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) Queues() core.Queues { return s.queues }

// Topics returns the broadcast topics.
//
//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) Topics() core.Topics { return s.topics }

// Resources returns the resource loader.
//
//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) Resources() core.ResourceLoader { return s.resources }

// AgentMemory returns the agent memory store.
//
//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) AgentMemory() core.AgentMemory { return s.memory }

// Traces returns the trace publisher.
//
//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) Traces() core.TracePublisher { return s.traces }

// Close stops every background loop, flushes what is buffered, and releases the
// connections.
//
// The order matters: the loops stop first so nothing new is published, then the
// trace publisher drains, then the transport goes. core.TracePublisher does not
// name Close — the buffered publisher has one and the no-op does not — so it is
// type-asserted, as the k8s module does.
func (s *Services) Close() error {
	s.cancel()
	var err error
	if closer, ok := s.traces.(interface{ Close() error }); ok {
		err = closer.Close()
	}
	s.client.close()
	return err
}
