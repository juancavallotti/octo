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
	if meta.InternalURL != "https://octo-int-orders.octo-dev.svc" {
		t.Errorf("internalUrl = %q, want the https address", meta.InternalURL)
	}
	if meta.InternalHTTPURL != "http://octo-int-orders.octo-dev:9090" {
		t.Errorf("internalHttpUrl = %q, want the plain address", meta.InternalHTTPURL)
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

// TestDeployInternalTLSIgnoredWithoutHTTPSource mirrors the external fallback: a
// workload with nothing to put TLS on deploys as it would have anyway.
func TestDeployInternalTLSIgnoredWithoutHTTPSource(t *testing.T) {
	repo := &fakeRepo{created: Deployment{ID: "dep-1"}}
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Ticker", Definition: bareDef}}
	kc := &fakeKube{status: kube.StatusPending, internalTLSEnabled: true}
	svc := NewService(repo, integrations, kc)

	if _, err := svc.Deploy(context.Background(), "int-1", Settings{InternalTLS: true}); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if kc.gotSpec.InternalTLS || ParseSettings(repo.gotSettings).InternalTLS {
		t.Error("internal TLS kept on a deployment with no HTTP source")
	}
}
