package kube

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayfake "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned/fake"
)

// gatewayConfig is testConfig plus the settings that switch endpoints onto
// Gateway API. It keeps the Ingress fields the base config sets, deliberately:
// a values file mid-migration carries both, and none of them may leak into the
// route.
func gatewayConfig(baseDomain string) Config {
	cfg := testConfig(baseDomain)
	cfg.EndpointAPI = EndpointAPIGateway
	cfg.Gateway = GatewayRef{Name: "octo-gateway", Namespace: "ingress", SectionName: "https"}
	return cfg
}

// testGatewayClient builds a Client in Gateway API mode and hands back the fake
// gateway clientset too, since the routes it creates live there rather than in
// the core clientset the other tests read.
func testGatewayClient(cfg Config, objects ...runtime.Object) (*Client, *gatewayfake.Clientset) {
	gw := gatewayfake.NewSimpleClientset()
	return newClient(cfg, fake.NewSimpleClientset(objects...), gw), gw
}

// exposedSpec is the deployment shape every endpoint test uses: networked,
// exposed, on a known subdomain.
func exposedSpec() Spec {
	return Spec{
		ID: "d1", IntegrationID: "int-1", Definition: "x: 1", Replicas: 1,
		Slug: "orders", Port: 9090, Expose: true, Subdomain: "shop",
	}
}

func TestApplyCreatesHTTPRouteWhenExposed(t *testing.T) {
	c, gw := testGatewayClient(gatewayConfig("octo.example.com"))
	ctx := context.Background()

	if err := c.Apply(ctx, exposedSpec()); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	name := resourceName("d1")
	rt, err := gw.GatewayV1().HTTPRoutes(testNamespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get httproute: %v", err)
	}
	if len(rt.Spec.Hostnames) != 1 || rt.Spec.Hostnames[0] != "shop.octo.example.com" {
		t.Errorf("hostnames = %v, want [shop.octo.example.com]", rt.Spec.Hostnames)
	}
	if len(rt.Spec.ParentRefs) != 1 {
		t.Fatalf("parentRefs = %v, want exactly one", rt.Spec.ParentRefs)
	}
	parent := rt.Spec.ParentRefs[0]
	if parent.Name != "octo-gateway" {
		t.Errorf("parentRef name = %q, want octo-gateway", parent.Name)
	}
	if parent.Namespace == nil || *parent.Namespace != "ingress" {
		t.Errorf("parentRef namespace = %v, want ingress", parent.Namespace)
	}
	if parent.SectionName == nil || *parent.SectionName != "https" {
		t.Errorf("parentRef sectionName = %v, want https", parent.SectionName)
	}
	if len(rt.Spec.Rules) != 1 {
		t.Fatalf("rules = %v, want exactly one", rt.Spec.Rules)
	}
	backend := rt.Spec.Rules[0].BackendRefs[0].BackendObjectReference
	if backend.Name != gatewayv1.ObjectName(name) {
		t.Errorf("backend = %q, want %q", backend.Name, name)
	}
	if backend.Port == nil || *backend.Port != 9090 {
		t.Errorf("backend port = %v, want 9090", backend.Port)
	}
	match := rt.Spec.Rules[0].Matches[0]
	if match.Path == nil || *match.Path.Type != gatewayv1.PathMatchPathPrefix || *match.Path.Value != "/" {
		t.Errorf("path match = %+v, want PathPrefix /", match.Path)
	}
	// The whole point of the mode: the certificate and the controller live on the
	// Gateway's listener, so nothing about either appears on the route.
	if len(rt.Annotations) != 0 {
		t.Errorf("route annotations = %v, want none", rt.Annotations)
	}
	// And no Ingress: the two modes are alternatives, not layers.
	if _, err := c.clientset.NetworkingV1().Ingresses(testNamespace).Get(ctx, name, metav1.GetOptions{}); err == nil {
		t.Error("gateway mode must not also create an Ingress")
	}
	// Labelled like every other resource of the deployment, so it is found and
	// cleaned up the same way.
	if rt.Labels[labelDeploymentID] != "d1" || rt.Labels[labelManagedBy] != managedByValue {
		t.Errorf("route labels = %v, want the deployment's labels", rt.Labels)
	}
}

// A Gateway in the orchestrator's own namespace leaves parentRef.namespace unset:
// that is what the field already means, and setting it would render the local and
// remote cases differently for no behavioural difference.
func TestRouteParentRefOmitsNamespaceWhenLocal(t *testing.T) {
	cfg := gatewayConfig("octo.example.com")
	cfg.Gateway.Namespace = testNamespace
	c, gw := testGatewayClient(cfg)
	ctx := context.Background()

	if err := c.Apply(ctx, exposedSpec()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	rt, err := gw.GatewayV1().HTTPRoutes(testNamespace).Get(ctx, resourceName("d1"), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get httproute: %v", err)
	}
	if ns := rt.Spec.ParentRefs[0].Namespace; ns != nil {
		t.Errorf("parentRef namespace = %q, want unset for a Gateway in our own namespace", *ns)
	}
}

// An unset sectionName is legal — it attaches to every listener that accepts the
// hostname — so it must render as absent rather than as an empty string, which
// names a listener called "".
func TestRouteParentRefOmitsSectionNameWhenUnset(t *testing.T) {
	cfg := gatewayConfig("octo.example.com")
	cfg.Gateway.SectionName = ""
	c, gw := testGatewayClient(cfg)
	ctx := context.Background()

	if err := c.Apply(ctx, exposedSpec()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	rt, _ := gw.GatewayV1().HTTPRoutes(testNamespace).Get(ctx, resourceName("d1"), metav1.GetOptions{})
	if sn := rt.Spec.ParentRefs[0].SectionName; sn != nil {
		t.Errorf("parentRef sectionName = %q, want unset", *sn)
	}
}

func TestApplyNoHTTPRouteWithoutBaseDomain(t *testing.T) {
	cfg := gatewayConfig("")
	cfg.Gateway = GatewayRef{} // no endpoints to publish, so no Gateway is needed
	c, gw := testGatewayClient(cfg)
	ctx := context.Background()

	if err := c.Apply(ctx, exposedSpec()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, err := gw.GatewayV1().HTTPRoutes(testNamespace).Get(ctx, resourceName("d1"), metav1.GetOptions{}); err == nil {
		t.Error("httproute should not be created when no base domain is configured")
	}
}

// Undeploy has to remove the route, and has to stay retryable: a deployment that
// was never exposed has no route to remove, and Delete is called again after a
// partial failure.
func TestDeleteWithdrawsHTTPRoute(t *testing.T) {
	c, gw := testGatewayClient(gatewayConfig("octo.example.com"))
	ctx := context.Background()

	if err := c.Apply(ctx, exposedSpec()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := c.Delete(ctx, "d1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := gw.GatewayV1().HTTPRoutes(testNamespace).Get(ctx, resourceName("d1"), metav1.GetOptions{}); err == nil {
		t.Error("httproute should be gone after Delete")
	}
	if err := c.Delete(ctx, "d1"); err != nil {
		t.Errorf("second Delete should be a no-op, got %v", err)
	}
}

// Gateway API is CRDs, not a built-in group, so "configured for it" and "the
// cluster has it" are independent facts. Preflight is what turns the mismatch
// into one startup error instead of a first deploy that fails much later.
func TestPreflightRequiresGatewayAPICRDs(t *testing.T) {
	c, gw := testGatewayClient(gatewayConfig("octo.example.com"))
	ctx := context.Background()

	if err := c.Preflight(ctx); err == nil {
		t.Fatal("preflight should fail on a cluster with no Gateway API CRDs")
	}

	gw.Resources = []*metav1.APIResourceList{{
		GroupVersion: gatewayv1.GroupVersion.String(),
		APIResources: []metav1.APIResource{{Name: "httproutes", Kind: "HTTPRoute", Namespaced: true}},
	}}
	if err := c.Preflight(ctx); err != nil {
		t.Errorf("preflight should pass once the CRDs are present, got %v", err)
	}
}

// A cluster serving the group but not HTTPRoute is a partial CRD install; it must
// not read as success, because every exposed deploy would then fail individually.
func TestPreflightRequiresHTTPRouteKind(t *testing.T) {
	c, gw := testGatewayClient(gatewayConfig("octo.example.com"))
	gw.Resources = []*metav1.APIResourceList{{
		GroupVersion: gatewayv1.GroupVersion.String(),
		APIResources: []metav1.APIResource{{Name: "gateways", Kind: "Gateway", Namespaced: true}},
	}}
	if err := c.Preflight(context.Background()); err == nil {
		t.Error("preflight should fail when the group is served without HTTPRoute")
	}
}

// Ingress is built into every cluster, so its preflight has nothing to check and
// must never be the reason a startup fails.
func TestPreflightPassesForIngress(t *testing.T) {
	c := testClient()
	if err := c.Preflight(context.Background()); err != nil {
		t.Errorf("ingress preflight should always pass, got %v", err)
	}
}

func TestParseEndpointAPI(t *testing.T) {
	tests := []struct {
		in      string
		want    EndpointAPI
		wantErr bool
	}{
		{"", EndpointAPIIngress, false},
		{"ingress", EndpointAPIIngress, false},
		{"gateway", EndpointAPIGateway, false},
		{"Gateway", "", true},
		{"httproute", "", true},
	}
	for _, tt := range tests {
		got, err := ParseEndpointAPI(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseEndpointAPI(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && got != tt.want {
			t.Errorf("ParseEndpointAPI(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// The name has no default worth guessing, and a route with an empty parentRef
// name attaches to nothing while reporting no error anywhere the operator looks.
func TestConfigValidateGatewayNeedsAName(t *testing.T) {
	cfg := gatewayConfig("octo.example.com")
	cfg.Gateway.Name = ""
	if err := cfg.Validate(); err == nil {
		t.Error("gateway mode with a base domain and no Gateway name should not validate")
	}

	// Without a base domain nothing is ever published, so there is nothing to
	// attach and no reason to demand a Gateway.
	off := gatewayConfig("")
	off.Gateway = GatewayRef{}
	if err := off.Validate(); err != nil {
		t.Errorf("gateway mode without external endpoints should validate, got %v", err)
	}

	if err := testConfig("octo.example.com").Validate(); err != nil {
		t.Errorf("ingress mode should validate, got %v", err)
	}
}

// newClient selects Ingress for anything that is not gateway, so an unrecognised
// value would otherwise mean "publish Ingresses" on a cluster configured for
// neither. main.go reaches ParseEndpointAPI first; this closes the same hole for
// a Config assembled in code.
func TestConfigValidateRejectsUnknownEndpointAPI(t *testing.T) {
	cfg := testConfig("octo.example.com")
	cfg.EndpointAPI = "httproute"
	if err := cfg.Validate(); err == nil {
		t.Error("an unrecognised EndpointAPI should not validate")
	}
	// The zero value is the documented default, not a mistake.
	cfg.EndpointAPI = ""
	if err := cfg.Validate(); err != nil {
		t.Errorf("the zero value should validate as ingress, got %v", err)
	}
}
