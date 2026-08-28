package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/services"
)

// resolution is one row of the degradation matrix: which accessor to read, and
// what it should hold under each policy.
type resolution struct {
	feature  Feature
	pick     func(*Services) any
	noop     any
	erroring any
}

// The degradation matrix is this module's actual contract with an operator: it is
// what they get when they have not implemented something. Stating it exhaustively
// is cheap, and it is the one table that must not drift silently.
func TestUnsupportedFeatureResolution(t *testing.T) {
	for _, tc := range degradationMatrix() {
		t.Run(string(tc.feature), func(t *testing.T) {
			// Absent from the document entirely: the per-feature default applies.
			want := tc.noop
			if defaultPolicy[tc.feature] == PolicyError {
				want = tc.erroring
			}
			assertResolves(t, tc, "", want)
			// Explicitly asking to degrade, and explicitly asking to refuse.
			assertResolves(t, tc, string(PolicyNoop), tc.noop)
			assertResolves(t, tc, string(PolicyError), tc.erroring)
		})
	}
}

func degradationMatrix() []resolution {
	return []resolution{
		{FeatureKV, func(s *Services) any { return s.KV() }, core.NoopKV(), erroringKV{}},
		{
			FeatureResources, func(s *Services) any { return s.Resources() },
			core.NoopResourceLoader{}, erroringResources{},
		},
		{FeatureLeases, func(s *Services) any { return s.Leases() }, core.NoopLeases(), erroringLeases{}},
		{
			FeatureLeaderElection, func(s *Services) any { return s.LeaderElection() },
			core.NoopLeaderElection(), erroringLeaderElection{},
		},
		{FeatureQueues, func(s *Services) any { return s.Queues() }, core.NoopQueues(), erroringQueues{}},
		{FeatureTopics, func(s *Services) any { return s.Topics() }, core.NoopTopics(), erroringTopics{}},
	}
}

// assertResolves builds the module against a document where one feature is
// unsupported and checks what the accessor returns.
func assertResolves(t *testing.T, tc resolution, policy string, want any) {
	t.Helper()
	doc := fullDiscovery()
	unsupport(&doc, tc.feature, policy)
	svc := newTestServices(t, newFake(t, doc), nil)
	if got, wantType := typeOf(tc.pick(svc)), typeOf(want); got != wantType {
		t.Fatalf("%s unsupported with policy %q resolved to %s, want %s",
			tc.feature, policy, got, wantType)
	}
}

// typeOf renders a value's dynamic type, ignoring pointer-ness, for comparison.
func typeOf(v any) string { return strings.TrimPrefix(fmt.Sprintf("%T", v), "*") }

// unsupport marks one feature unsupported in doc, with an optional policy.
func unsupport(doc *discoveryDocument, feature Feature, policy string) {
	off := featureFlags{Supported: false, Unsupported: policy}
	switch feature {
	case FeatureKV:
		doc.Features.KV.featureFlags = off
	case FeatureSecrets:
		doc.Features.Secrets.featureFlags = off
	case FeatureResources:
		doc.Features.Resources = off
	case FeatureLeases:
		doc.Features.Leases.featureFlags = off
	case FeatureLeaderElection:
		doc.Features.LeaderElection.featureFlags = off
	case FeatureQueues:
		doc.Features.Queues.featureFlags = off
	case FeatureTopics:
		doc.Features.Topics.featureFlags = off
	case FeatureAgentMemory:
		doc.Features.AgentMemory.featureFlags = off
	case FeatureTraces:
		doc.Features.Traces.featureFlags = off
	case FeatureLogs:
		doc.Features.Logs = off
	case FeatureCore:
	}
}

// Leases and leader election refuse rather than degrade when the server says
// nothing, because degrading means granting: every replica would be told it holds
// the claim, and each would run the work the claim exists to run once.
func TestLeasesAndLeadershipRefuseByDefault(t *testing.T) {
	doc := fullDiscovery()
	unsupport(&doc, FeatureLeases, "")
	unsupport(&doc, FeatureLeaderElection, "")
	svc := newTestServices(t, newFake(t, doc), nil)

	if _, ok, err := svc.Leases().Acquire(t.Context(), "job"); err == nil || ok {
		t.Fatalf("Acquire = (ok %v, err %v), want a refusal", ok, err)
	}
	if _, err := svc.LeaderElection().Acquire(t.Context(), "job"); err == nil {
		t.Fatal("leader Acquire err = nil, want a refusal")
	}
}

// "I store values but not secrets" is a statement a platform should be able to
// make and have honoured, rather than having its secrets written beside
// everything else.
func TestSecretsRefusedIndependentlyOfKV(t *testing.T) {
	doc := fullDiscovery()
	doc.Features.Secrets.featureFlags = featureFlags{Supported: false, Unsupported: string(PolicyError)}
	svc := newTestServices(t, newFake(t, doc), nil)

	if _, err := svc.Secrets().Set(t.Context(), core.NamespaceUser, "k", []byte("v"), 0); err == nil {
		t.Fatal("Set err = nil, want a refusal")
	}
}

// Agent memory, traces and logs have no erroring mode: the engine branches on
// Enabled() before it would ever reach a write, so an erroring store would differ
// from the no-op only in code nobody runs.
func TestFeaturesWithoutAnErroringMode(t *testing.T) {
	for feature, want := range fixedPolicy {
		if got := policyFor(feature, string(PolicyError)); got != want {
			t.Fatalf("policyFor(%s, error) = %s, want %s", feature, got, want)
		}
	}
}

// An unrecognized policy is a typo in somebody's document. Defaulting is friendlier
// than refusing to start over one.
func TestUnrecognizedPolicyFallsBackToTheDefault(t *testing.T) {
	if got := policyFor(FeatureKV, "sometimes"); got != defaultPolicy[FeatureKV] {
		t.Fatalf("policyFor(kv, sometimes) = %s, want %s", got, defaultPolicy[FeatureKV])
	}
}

// A server that answers discovery late still gets a working runtime. This is what
// makes the sidecar deployment work, where container start order is not
// guaranteed and the first attempts hit a closed port.
func TestDiscoveryRetriesWithinTheBudget(t *testing.T) {
	f := newFake(t, fullDiscovery())
	f.discoveryFails = 2
	svc := newTestServices(t, f, map[string]string{envDiscoveryBudget: "5s"})

	if svc.doc.Implementation.Name != "fake" {
		t.Fatalf("implementation = %q, want the document served after the retries",
			svc.doc.Implementation.Name)
	}
	if got := f.count("GET /v1/discovery"); got != 3 {
		t.Fatalf("discovery calls = %d, want 3 (two refusals then the answer)", got)
	}
}

// Past the budget the default is to refuse to start: a runtime that cannot reach
// its platform is not a working runtime, and that is how the other modules behave.
func TestDiscoveryFailureRefusesStartupByDefault(t *testing.T) {
	f := newFake(t, fullDiscovery())
	f.discoveryFails = 1000
	t.Setenv(envURL, f.url())
	t.Setenv(envDiscoveryBudget, "50ms")

	_, err := New(t.Context(), services.Options{})
	if err == nil {
		t.Fatal("New err = nil, want a startup failure")
	}
	// The error has to name the way out, because the reader is somebody whose
	// deployment just failed to start.
	if !strings.Contains(err.Error(), envStartup) {
		t.Fatalf("New err = %v, want it to name %s as the way to start degraded", err, envStartup)
	}
}

// The degrade policy trades capability for availability: the runtime starts, and
// every platform capability reports unavailable in its own idiom.
func TestDiscoveryFailureCanDegradeInstead(t *testing.T) {
	f := newFake(t, fullDiscovery())
	f.discoveryFails = 1000
	svc := newTestServices(t, f, map[string]string{
		envDiscoveryBudget: "50ms",
		envStartup:         StartupDegrade,
	})

	_, err := svc.KV().Set(t.Context(), core.NamespaceUser, "k", []byte("v"), 0)
	if !errors.Is(err, core.ErrNoKV) {
		t.Fatalf("KV Set err = %v, want ErrNoKV", err)
	}
	if _, _, err := svc.Leases().Acquire(t.Context(), "job"); err == nil {
		t.Fatal("Acquire err = nil, want a refusal: leases refuse rather than grant")
	}
}

// A discovery endpoint answering 501 is a server saying it implements nothing,
// which is an empty answer rather than a failure — so the runtime starts, fully
// degraded, instead of refusing.
func TestDiscoveryNotImplementedIsAnEmptyContract(t *testing.T) {
	f := newFake(t, discoveryDocument{})
	f.mux.HandleFunc("GET /v1/", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not implemented", http.StatusNotImplemented)
	})
	t.Setenv(envURL, f.url())

	svc, err := New(t.Context(), services.Options{})
	if err != nil {
		t.Fatalf("New: %v, want a degraded start rather than a failure", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if _, ok, err := svc.KV().Get(t.Context(), core.NamespaceUser, "k"); ok || err != nil {
		t.Fatalf("KV Get = (ok %v, err %v), want a silent miss", ok, err)
	}
}

// The base URL is the one setting with no defensible default.
func TestMissingBaseURLIsRefused(t *testing.T) {
	t.Setenv(envURL, "")
	if _, err := New(t.Context(), services.Options{}); err == nil {
		t.Fatal("New err = nil, want a failure naming the required URL")
	}
}

// The module registers itself under its own name, which is what makes
// RUNTIME_SERVICES_MODULE=api select it.
func TestModuleName(t *testing.T) {
	if Module != "api" {
		t.Fatalf("Module = %q, want api", Module)
	}
}
