package deployment

import (
	"context"
	"errors"
	"testing"

	"github.com/juancavallotti/octo/orchestrator/internal/integration"
	"github.com/juancavallotti/octo/orchestrator/internal/kube"
)

// bareDef has no HTTP source, so it deploys as a workload with no address.
const bareDef = "flows:\n  - name: tick\n    source:\n      type: cron\n"

func TestDeployInternalTLS(t *testing.T) {
	repo := &fakeRepo{created: Deployment{ID: "dep-1"}}
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Orders", Definition: exposableDef}}
	kc := &fakeKube{status: kube.StatusPending, internalTLSEnabled: true}
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
	kc := &fakeKube{status: kube.StatusPending, internalTLSEnabled: true, runtimeVersion: "0.11.18"}
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
