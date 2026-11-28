package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/juancavallotti/octo/orchestrator/internal/integration"
	"github.com/juancavallotti/octo/orchestrator/internal/kube"
	"github.com/juancavallotti/octo/orchestrator/internal/snapshot"
)

// tlsRelease is an installation release that serves internal TLS.
const tlsRelease = "0.12.0"

// bareDef has no HTTP source, so it deploys as a workload with no address.
const bareDef = "flows:\n  - name: tick\n    source:\n      type: cron\n"

func TestDeployInternalTLS(t *testing.T) {
	repo := &fakeRepo{created: Deployment{ID: "dep-1"}}
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Orders", Definition: exposableDef}}
	kc := &fakeKube{status: kube.StatusPending, internalTLSEnabled: true, runtimeVersion: tlsRelease}
	svc := NewService(repo, integrations, kc)

	if _, err := svc.Deploy(context.Background(), "int-1", Settings{InternalTLS: true}); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if !kc.gotSpec.InternalTLS {
		t.Error("spec does not ask for internal TLS")
	}
	if !ParseSettings(repo.gotSettings).InternalTLS {
		t.Error("internalTls was not persisted, so a rollout would drop it")
	}
	meta := ParseMetadata(repo.gotMetadata)
	if meta.InternalURL != "https://octo-int-orders.octo-dev:8443" {
		t.Errorf("internalUrl = %q, want the https address", meta.InternalURL)
	}
}

func TestDeployInternalTLSUnavailable(t *testing.T) {
	repo := &fakeRepo{created: Deployment{ID: "dep-1"}}
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Orders", Definition: exposableDef}}
	svc := NewService(repo, integrations, &fakeKube{status: kube.StatusPending})

	_, err := svc.Deploy(context.Background(), "int-1", Settings{InternalTLS: true})
	if !errors.Is(err, ErrInternalTLSUnavailable) {
		t.Fatalf("err = %v, want ErrInternalTLSUnavailable", err)
	}
}

// TestDeployInternalTLSRefusesOldRuntime pins the gate: a pod on a release that
// ignores HTTPS_PORT would leave the HTTPS Service with nothing behind it.
func TestDeployInternalTLSRefusesOldRuntime(t *testing.T) {
	repo := &fakeRepo{created: Deployment{ID: "dep-1"}}
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Orders", Definition: exposableDef}}
	kc := &fakeKube{status: kube.StatusPending, internalTLSEnabled: true, runtimeVersion: "0.12.0"}
	svc := NewService(repo, integrations, kc)

	_, err := svc.Deploy(context.Background(), "int-1", Settings{InternalTLS: true, RuntimeVersion: "0.11.14"})
	if !errors.Is(err, ErrInternalTLSRuntime) {
		t.Fatalf("err = %v, want ErrInternalTLSRuntime", err)
	}
	if _, err := svc.Deploy(context.Background(), "int-1", Settings{RuntimeVersion: "0.11.14"}); err != nil {
		t.Fatalf("plain deploy on the same runtime: %v", err)
	}
}

// TestDeployInternalTLSIgnoredWithoutHTTPSource mirrors the external fallback: a
// workload with nothing to put TLS on deploys as it would have anyway.
func TestDeployInternalTLSIgnoredWithoutHTTPSource(t *testing.T) {
	repo := &fakeRepo{created: Deployment{ID: "dep-1"}}
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Ticker", Definition: bareDef}}
	// Unavailable on the installation too: a deploy that would ignore the setting
	// is not refused for it.
	kc := &fakeKube{status: kube.StatusPending}
	svc := NewService(repo, integrations, kc)

	if _, err := svc.Deploy(context.Background(), "int-1", Settings{InternalTLS: true}); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if kc.gotSpec.InternalTLS || ParseSettings(repo.gotSettings).InternalTLS {
		t.Error("internal TLS kept on a deployment with no HTTP source")
	}
}

// TestDeployInternalTLSFollowsTheInstallRelease pins that a deployment following
// the install is judged by the install's release, and that one nobody can name is
// refused rather than assumed new.
func TestDeployInternalTLSFollowsTheInstallRelease(t *testing.T) {
	cases := []struct {
		name string
		kc   *fakeKube
		ok   bool
	}{
		{"a TLS install", &fakeKube{runtimeVersion: tlsRelease}, true},
		{"an older install", &fakeKube{runtimeVersion: "0.11.17"}, false},
		{"an install of unknown release", &fakeKube{unknownRelease: true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.kc.status, tc.kc.internalTLSEnabled = kube.StatusPending, true
			integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Orders", Definition: exposableDef}}
			svc := NewService(&fakeRepo{created: Deployment{ID: "dep-1"}}, integrations, tc.kc)

			_, err := svc.Deploy(context.Background(), "int-1", Settings{InternalTLS: true})
			if tc.ok && err != nil {
				t.Fatalf("Deploy: %v", err)
			}
			if !tc.ok && !errors.Is(err, ErrInternalTLSRuntime) {
				t.Fatalf("err = %v, want ErrInternalTLSRuntime", err)
			}
		})
	}
}

// tlsDef is exposableDef with a tls block on its connector.
func tlsDef(tls string) string {
	return "env:\n  - name: HTTP_PORT\n    default: \"9090\"\n" +
		"connectors:\n  - name: api\n    type: http\n    settings:\n      port: ${HTTP_PORT}\n" +
		"      tls:\n" + tls +
		"flows:\n  - name: api\n    source:\n      connector: api\n      type: http\n"
}

// TestDeployRefusesConnectorTLSTheServicesCannotReach pins both mismatches
// between a connector's TLS and the deployment's Services, and the shapes that
// fit.
func TestDeployRefusesConnectorTLSTheServicesCannotReach(t *testing.T) {
	cases := []struct {
		name        string
		tls         string
		internalTLS bool
		ok          bool
	}{
		{"a second HTTPS port under internal TLS", "        port: 9443\n", true, false},
		{"HTTPS on the HTTP port without internal TLS", "        enabled: true\n", false, false},
		{"a certificate on the HTTP port without internal TLS", "        certFile: /tls/tls.crt\n        keyFile: /tls/tls.key\n", false, false},
		{"a second HTTPS port without internal TLS", "        enabled: true\n        port: 9443\n", false, true},
		{"a bare tls block under internal TLS", "        minVersion: \"1.3\"\n", true, true},
		{"a bare tls block without internal TLS", "        minVersion: \"1.3\"\n", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			def := tlsDef(tc.tls)
			if !Exposable(def) {
				t.Fatal("fixture is not wired to the injected port")
			}
			kc := &fakeKube{status: kube.StatusPending, internalTLSEnabled: true, runtimeVersion: tlsRelease}
			integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Orders", Definition: def}}
			svc := NewService(&fakeRepo{created: Deployment{ID: "dep-1"}}, integrations, kc)

			_, err := svc.Deploy(context.Background(), "int-1", Settings{InternalTLS: tc.internalTLS})
			if tc.ok && err != nil {
				t.Fatalf("Deploy: %v", err)
			}
			if !tc.ok && !errors.Is(err, ErrConnectorTLS) {
				t.Fatalf("err = %v, want ErrConnectorTLS", err)
			}
			if !tc.ok && kc.applied {
				t.Error("the workload was created before the refusal")
			}
		})
	}
}

// tlsDeployment is a networked deployment row serving internal TLS.
func tlsDeployment() Deployment {
	return Deployment{
		ID: "dep-1", IntegrationID: "int-1",
		Settings: json.RawMessage(`{"replicas":1,"internalTls":true}`),
		Metadata: json.RawMessage(`{"name":"Orders","slug":"orders"}`),
	}
}

// TestRolloutRefusesInternalTLSOnceTheIssuerIsGone pins that a rollout does not
// replace HTTPS pods with ones that cannot serve what the Services point at.
func TestRolloutRefusesInternalTLSOnceTheIssuerIsGone(t *testing.T) {
	snaps := &fakeSnapshots{ret: snapshot.Snapshot{ID: "snap-2", IntegrationID: "int-1", Definition: exposableDef}}
	kc := &fakeKube{status: kube.StatusRunning, runtimeVersion: tlsRelease}
	svc := NewService(&fakeRepo{getRet: tlsDeployment()}, &fakeIntegrations{}, kc, WithSnapshots(snaps))

	_, err := svc.Rollout(context.Background(), "dep-1", RolloutChange{SnapshotID: "snap-2"})
	if !errors.Is(err, ErrInternalTLSUnavailable) {
		t.Fatalf("err = %v, want ErrInternalTLSUnavailable", err)
	}
	if kc.rolledOut {
		t.Error("the workload was rolled out")
	}
}

func TestRolloutInternalTLSChecks(t *testing.T) {
	cases := []struct {
		name string
		def  string
		kc   *fakeKube
		want error
	}{
		{"a TLS install", exposableDef, &fakeKube{runtimeVersion: tlsRelease}, nil},
		{"an older install", exposableDef, &fakeKube{runtimeVersion: "0.11.17"}, ErrInternalTLSRuntime},
		{"a second HTTPS port", tlsDef("        port: 9443\n"), &fakeKube{runtimeVersion: tlsRelease}, ErrConnectorTLS},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.kc.status, tc.kc.internalTLSEnabled = kube.StatusRunning, true
			snaps := &fakeSnapshots{ret: snapshot.Snapshot{ID: "snap-2", IntegrationID: "int-1", Definition: tc.def}}
			svc := NewService(&fakeRepo{getRet: tlsDeployment()}, &fakeIntegrations{}, tc.kc, WithSnapshots(snaps))

			_, err := svc.Rollout(context.Background(), "dep-1", RolloutChange{SnapshotID: "snap-2"})
			if tc.want == nil && err != nil {
				t.Fatalf("Rollout: %v", err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}
