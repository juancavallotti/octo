package kube

import (
	"context"
	"slices"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	gatewayfake "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned/fake"
)

const (
	testIssuer      = "octo-internal-ca"
	testCAConfigMap = "octo-internal-ca"
)

// tlsConfig is testConfig with internal TLS configured, on ingress class class.
func tlsConfig(class string) Config {
	cfg := testConfig("octo.example.com")
	cfg.IngressClass = class
	cfg.InternalTLSIssuer = testIssuer
	cfg.InternalCAConfigMap = testCAConfigMap
	return cfg
}

// ingressClass is an IngressClass named name belonging to controller.
func ingressClass(name, controller string, isDefault bool) *networkingv1.IngressClass {
	ret := &networkingv1.IngressClass{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       networkingv1.IngressClassSpec{Controller: controller},
	}
	if isDefault {
		ret.Annotations = map[string]string{defaultIngressClass: "true"}
	}
	return ret
}

// testIngressClasses are the classes the TLS tests' cluster has, each named
// after its controller as a stock install names it.
func testIngressClasses() []runtime.Object {
	return []runtime.Object{
		ingressClass("traefik", traefikControllerName, false),
		ingressClass("nginx", nginxControllerName, false),
		ingressClass("haproxy", "haproxy.org/ingress-controller", false),
	}
}

// gatewayResources is the Gateway API v1.4+ discovery a TLS endpoint needs.
func gatewayResources(kinds ...string) []*metav1.APIResourceList {
	resources := make([]metav1.APIResource, 0, len(kinds))
	for _, k := range kinds {
		resources = append(resources, metav1.APIResource{Kind: k, Namespaced: true})
	}
	return []*metav1.APIResourceList{{GroupVersion: "gateway.networking.k8s.io/v1", APIResources: resources}}
}

// tlsClient builds a Client from cfg and returns its fakes, since the objects
// internal TLS writes are spread across all three. objects seed the cluster,
// which otherwise has testIngressClasses.
func tlsClient(
	cfg Config, objects ...runtime.Object,
) (*Client, *fake.Clientset, *gatewayfake.Clientset, func(string) *unstructured.Unstructured) {
	if len(objects) == 0 {
		objects = testIngressClasses()
	}
	cs := fake.NewSimpleClientset(objects...)
	gw := gatewayfake.NewSimpleClientset()
	gw.Resources = gatewayResources("HTTPRoute", "BackendTLSPolicy")
	dyn := testDynamic()
	get := func(name string) *unstructured.Unstructured {
		obj, err := dyn.Resource(certificateGVR).Namespace(testNamespace).Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return nil
		}
		return obj
	}
	return newClient(cfg, cs, gw, dyn), cs, gw, get
}

func tlsSpec() Spec {
	s := exposedSpec()
	s.InternalTLS = true
	return s
}

func TestApplyInternalTLSIssuesCertificate(t *testing.T) {
	c, _, _, getCert := tlsClient(tlsConfig("traefik"))
	if err := c.Apply(context.Background(), tlsSpec()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	cert := getCert(internalTLSSecretName("d1"))
	if cert == nil {
		t.Fatal("no Certificate was created")
	}
	issuer, _, _ := unstructured.NestedString(cert.Object, "spec", "issuerRef", "name")
	if issuer != testIssuer {
		t.Errorf("issuer = %q, want %q", issuer, testIssuer)
	}
	names, _, _ := unstructured.NestedStringSlice(cert.Object, "spec", "dnsNames")
	for _, want := range []string{
		"octo-int-orders", "octo-int-orders.octo-dev.svc", "octo-dep-d1.octo-dev.svc.cluster.local",
	} {
		if !slices.Contains(names, want) {
			t.Errorf("dnsNames %v lack %q", names, want)
		}
	}
}

func TestApplyInternalTLSServesHTTPSOnly(t *testing.T) {
	c, cs, _, _ := tlsClient(tlsConfig("traefik"))
	ctx := context.Background()
	if err := c.Apply(ctx, tlsSpec()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	for _, name := range []string{resourceName("d1"), internalServiceName("orders")} {
		svc, err := cs.CoreV1().Services(testNamespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("get service %s: %v", name, err)
		}
		if got := portNames(svc.Spec.Ports); !slices.Equal(got, []string{httpsPortName}) {
			t.Errorf("service %s ports = %v, want [https]", name, got)
		}
	}
}

func TestApplyInternalTLSPodWiring(t *testing.T) {
	c, cs, _, _ := tlsClient(tlsConfig("traefik"))
	ctx := context.Background()
	if err := c.Apply(ctx, tlsSpec()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	dep, err := cs.AppsV1().Deployments(testNamespace).Get(ctx, resourceName("d1"), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	pod := dep.Spec.Template.Spec
	ctr := pod.Containers[0]
	env := envMap(ctr.Env)
	if env[envHTTPSPort] != "8443" || env[envTLSCertFile] == "" || env[envTLSKeyFile] == "" {
		t.Errorf("TLS env missing: %v", env)
	}
	if env[envSSLCertDir] != systemCertDir+":"+caMountPath {
		t.Errorf("SSL_CERT_DIR = %q", env[envSSLCertDir])
	}
	if !slices.ContainsFunc(ctr.Ports, func(p corev1.ContainerPort) bool { return p.Name == httpsPortName }) {
		t.Errorf("container ports %v lack https", ctr.Ports)
	}
	if !hasVolume(pod.Volumes, tlsVolume) || !hasVolume(pod.Volumes, caVolume) {
		t.Errorf("volumes %v lack the certificate or the CA bundle", pod.Volumes)
	}
}

// TestApplyWithoutInternalTLSStillTrustsCA pins that a deployment that serves
// plain HTTP only can still verify the ones that serve HTTPS.
func TestApplyWithoutInternalTLSStillTrustsCA(t *testing.T) {
	c, cs, _, getCert := tlsClient(tlsConfig("traefik"))
	ctx := context.Background()
	if err := c.Apply(ctx, exposedSpec()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if getCert(internalTLSSecretName("d1")) != nil {
		t.Error("a Certificate was created for a deployment that did not ask for TLS")
	}
	dep, _ := cs.AppsV1().Deployments(testNamespace).Get(ctx, resourceName("d1"), metav1.GetOptions{})
	pod := dep.Spec.Template.Spec
	env := envMap(pod.Containers[0].Env)
	if _, ok := env[envHTTPSPort]; ok {
		t.Error("HTTPS_PORT set on a deployment without TLS")
	}
	if !hasVolume(pod.Volumes, caVolume) || env[envSSLCertDir] == "" {
		t.Error("the CA bundle is not trusted")
	}
}

func TestApplyInternalTLSIgnoredWithoutIssuer(t *testing.T) {
	c, cs, _, getCert := tlsClient(testConfig("octo.example.com"))
	ctx := context.Background()
	if err := c.Apply(ctx, tlsSpec()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if getCert(internalTLSSecretName("d1")) != nil {
		t.Error("a Certificate was created with no issuer configured")
	}
	svc, _ := cs.CoreV1().Services(testNamespace).Get(ctx, resourceName("d1"), metav1.GetOptions{})
	ports := svc.Spec.Ports
	if len(ports) != 1 || ports[0].Name != "http" || ports[0].Port != tlsSpec().port() {
		t.Errorf("service ports = %+v, want one http port on %d", ports, tlsSpec().port())
	}
}

func TestDeleteRemovesInternalCertificate(t *testing.T) {
	c, cs, _, getCert := tlsClient(tlsConfig("traefik"))
	ctx := context.Background()
	if err := c.Apply(ctx, tlsSpec()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, err := cs.CoreV1().Secrets(testNamespace).Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: internalTLSSecretName("d1")},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(ctx, "d1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if getCert(internalTLSSecretName("d1")) != nil {
		t.Error("Certificate left behind")
	}
	if _, err := cs.CoreV1().Secrets(testNamespace).Get(ctx, internalTLSSecretName("d1"), metav1.GetOptions{}); err == nil {
		t.Error("certificate Secret left behind")
	}
}

// TestDeleteWithoutInternalTLSTouchesNoCustomResources pins that undeploy on an
// installation without internal TLS never reaches for Certificates or
// ServersTransports, which its Role does not grant.
func TestDeleteWithoutInternalTLSTouchesNoCustomResources(t *testing.T) {
	cs := fake.NewSimpleClientset()
	dyn := testDynamic()
	c := newClient(testConfig("octo.example.com"), cs, gatewayfake.NewSimpleClientset(), dyn)
	ctx := context.Background()
	if err := c.Apply(ctx, exposedSpec()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := c.Delete(ctx, "d1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if n := len(dyn.Actions()); n != 0 {
		t.Errorf("dynamic client saw %d actions, want none: %v", n, dyn.Actions())
	}
}

func TestTraefikReencrypt(t *testing.T) {
	c, cs, _, _ := tlsClient(tlsConfig("traefik"))
	ctx := context.Background()
	if err := c.Apply(ctx, tlsSpec()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	name := resourceName("d1")
	transport, err := c.dynamic.Resource(serversTransportGVR).Namespace(testNamespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get serverstransport: %v", err)
	}
	if sn, _, _ := unstructured.NestedString(transport.Object, "spec", "serverName"); sn != "octo-dep-d1.octo-dev.svc" {
		t.Errorf("serverName = %q", sn)
	}
	svc, _ := cs.CoreV1().Services(testNamespace).Get(ctx, name, metav1.GetOptions{})
	if svc.Annotations[traefikServersScheme] != "https" ||
		svc.Annotations[traefikServersTransport] != testNamespace+"-"+name+"@kubernetescrd" {
		t.Errorf("service annotations = %v", svc.Annotations)
	}
	ing, _ := cs.NetworkingV1().Ingresses(testNamespace).Get(ctx, name, metav1.GetOptions{})
	if port := ing.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Port; port.Name != httpsPortName {
		t.Errorf("ingress backend port = %+v, want https", port)
	}

	if err := c.Delete(ctx, "d1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := c.dynamic.Resource(serversTransportGVR).Namespace(testNamespace).Get(ctx, name, metav1.GetOptions{}); err == nil {
		t.Error("ServersTransport left behind")
	}
}

func TestNginxReencrypt(t *testing.T) {
	c, cs, _, _ := tlsClient(tlsConfig("nginx"))
	ctx := context.Background()
	if err := c.Apply(ctx, tlsSpec()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	ing, _ := cs.NetworkingV1().Ingresses(testNamespace).Get(ctx, resourceName("d1"), metav1.GetOptions{})
	if ing.Annotations[nginxBackendProtocol] != "HTTPS" ||
		ing.Annotations[nginxProxySSLSecret] != testNamespace+"/"+internalTLSSecretName("d1") {
		t.Errorf("ingress annotations = %v", ing.Annotations)
	}
}

// TestUnknownIngressClassRefusesInternalTLS pins that a controller this
// orchestrator cannot configure is refused rather than routed to a port it
// cannot speak to.
func TestUnknownIngressClassRefusesInternalTLS(t *testing.T) {
	c, _, _, _ := tlsClient(tlsConfig("haproxy"))
	if err := c.Apply(context.Background(), tlsSpec()); err == nil {
		t.Fatal("Apply exposed an internal-TLS deployment through an ingress that cannot re-encrypt")
	}
}

func TestGatewayReencrypt(t *testing.T) {
	cfg := gatewayConfig("octo.example.com")
	cfg.InternalTLSIssuer = testIssuer
	cfg.InternalCAConfigMap = testCAConfigMap
	c, _, gw, _ := tlsClient(cfg)
	ctx := context.Background()
	if err := c.Apply(ctx, tlsSpec()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	name := resourceName("d1")
	policy, err := gw.GatewayV1().BackendTLSPolicies(testNamespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get backendtlspolicy: %v", err)
	}
	if string(policy.Spec.Validation.Hostname) != "octo-dep-d1.octo-dev.svc" ||
		string(policy.Spec.Validation.CACertificateRefs[0].Name) != testCAConfigMap {
		t.Errorf("validation = %+v", policy.Spec.Validation)
	}
	rt, _ := gw.GatewayV1().HTTPRoutes(testNamespace).Get(ctx, name, metav1.GetOptions{})
	if port := rt.Spec.Rules[0].BackendRefs[0].Port; port == nil || int32(*port) != httpsPort {
		t.Errorf("route backend port = %v, want 443", port)
	}
}

func TestPlatformURLsFollowRuntime(t *testing.T) {
	secure := PlatformURLs{Orchestrator: "https://orch.svc:8443", IAM: "http://iam:8093"}
	plain := PlatformURLs{Orchestrator: "http://orch:8090", IAM: "http://iam:8093"}
	cases := []struct {
		name, image, version, pin string
		want                      PlatformURLs
	}{
		{"pinned to a TLS release", "octo-runtime:dev", "", "0.12.0", secure},
		{"pinned before TLS", "octo-runtime:0.12.0", "", "0.11.14", plain},
		{"following a TLS install", "octo-runtime:0.11.17", "0.12.0", "", secure},
		{"following an older install", "octo-runtime:0.11.17", "", "", plain},
		{"following an install of unknown release", "octo-runtime@sha256:abc", "", "", plain},
	}
	for _, tc := range cases {
		cfg := testConfig("octo.example.com")
		cfg.RuntimeImage = tc.image
		cfg.RuntimeVersion = tc.version
		cfg.RuntimeServices = RuntimeServices{
			Module:          "k8s",
			OrchestratorURL: "http://orch:8090",
			IAMURL:          "http://iam:8093",
			HTTPS:           PlatformURLs{Orchestrator: "https://orch.svc:8443"},
		}
		if got := testClientFor(cfg).platformURLs(Spec{RuntimePin: tc.pin}); got != tc.want {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

// TestReencryptFollowsTheClassController pins that re-encrypt is chosen by the
// controller an IngressClass belongs to, whatever it is called, and that with no
// class configured it is the cluster default's.
func TestReencryptFollowsTheClassController(t *testing.T) {
	cases := []struct {
		name  string
		class string
		seed  *networkingv1.IngressClass
	}{
		{"a Traefik class with its own name", "public", ingressClass("public", traefikControllerName, false)},
		{"the cluster's default Traefik class", "", ingressClass("edge", traefikControllerName, true)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _, _, _ := tlsClient(tlsConfig(tc.class), tc.seed, ingressClass("nginx", nginxControllerName, false))
			ctx := context.Background()
			if err := c.Apply(ctx, tlsSpec()); err != nil {
				t.Fatalf("Apply: %v", err)
			}
			_, err := c.dynamic.Resource(serversTransportGVR).Namespace(testNamespace).
				Get(ctx, resourceName("d1"), metav1.GetOptions{})
			if err != nil {
				t.Errorf("no ServersTransport for a Traefik class: %v", err)
			}
		})
	}
}

func TestReencryptRefusesAClusterWithoutADefaultClass(t *testing.T) {
	c, _, _, _ := tlsClient(tlsConfig(""), ingressClass("traefik", traefikControllerName, false))
	if err := c.Apply(context.Background(), tlsSpec()); err == nil {
		t.Fatal("Apply re-encrypted through an ingress class nothing names")
	}
}

// TestReencryptObjectsAreOwnedByTheDeployment pins that what only some
// installations create is collected with the deployment, whatever the
// installation is configured for by the time it goes.
func TestReencryptObjectsAreOwnedByTheDeployment(t *testing.T) {
	owned := func(t *testing.T, what string, refs []metav1.OwnerReference) {
		t.Helper()
		if len(refs) != 1 || refs[0].Kind != "ConfigMap" || refs[0].Name != resourceName("d1") {
			t.Errorf("%s owner references = %+v, want the deployment's ConfigMap", what, refs)
		}
	}
	ctx := context.Background()
	t.Run("ingress", func(t *testing.T) {
		c, _, _, getCert := tlsClient(tlsConfig("traefik"))
		if err := c.Apply(ctx, tlsSpec()); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		owned(t, "Certificate", getCert(internalTLSSecretName("d1")).GetOwnerReferences())
		transport, err := c.dynamic.Resource(serversTransportGVR).Namespace(testNamespace).
			Get(ctx, resourceName("d1"), metav1.GetOptions{})
		if err != nil {
			t.Fatalf("get serverstransport: %v", err)
		}
		owned(t, "ServersTransport", transport.GetOwnerReferences())
	})
	t.Run("gateway", func(t *testing.T) {
		cfg := gatewayConfig("octo.example.com")
		cfg.InternalTLSIssuer = testIssuer
		cfg.InternalCAConfigMap = testCAConfigMap
		c, _, gw, _ := tlsClient(cfg)
		if err := c.Apply(ctx, tlsSpec()); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		policy, err := gw.GatewayV1().BackendTLSPolicies(testNamespace).Get(ctx, resourceName("d1"), metav1.GetOptions{})
		if err != nil {
			t.Fatalf("get backendtlspolicy: %v", err)
		}
		owned(t, "BackendTLSPolicy", policy.OwnerReferences)
	})
}

// TestGatewayTLSEndpointNeedsBackendTLSPolicy pins that a Gateway API older
// than the policy refuses a TLS endpoint with the version it needs named.
func TestGatewayTLSEndpointNeedsBackendTLSPolicy(t *testing.T) {
	cfg := gatewayConfig("octo.example.com")
	cfg.InternalTLSIssuer = testIssuer
	cfg.InternalCAConfigMap = testCAConfigMap
	c, _, gw, _ := tlsClient(cfg)
	gw.Resources = gatewayResources("HTTPRoute")
	ctx := context.Background()

	err := c.Apply(ctx, tlsSpec())
	if err == nil || !strings.Contains(err.Error(), "BackendTLSPolicy") {
		t.Fatalf("Apply = %v, want a missing BackendTLSPolicy named", err)
	}
	if err := c.Preflight(ctx); err != nil {
		t.Errorf("Preflight = %v; plain endpoints still work on this cluster", err)
	}
}

func TestValidateGatewayReencryptNeedsCABundle(t *testing.T) {
	cfg := gatewayConfig("octo.example.com")
	cfg.InternalTLSIssuer = testIssuer
	if err := cfg.Validate(); err == nil {
		t.Error("gateway re-encrypt with no CA bundle ConfigMap validated")
	}
	cfg.InternalCAConfigMap = testCAConfigMap
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
	cfg.InternalTLSIssuer, cfg.InternalCAConfigMap = "", ""
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate without internal TLS: %v", err)
	}
}

// TestCABundleIsRequired pins that a pod waits for the CA bundle rather than
// starting with a root pool that lacks it for the life of the process.
func TestCABundleIsRequired(t *testing.T) {
	c, _, _, _ := tlsClient(tlsConfig("traefik"))
	for _, v := range c.tlsVolumes(exposedSpec()) {
		if v.Name == caVolume && v.ConfigMap.Optional != nil && *v.ConfigMap.Optional {
			t.Error("the CA bundle volume is optional")
		}
	}
}

func portNames(ports []corev1.ServicePort) []string {
	out := make([]string, len(ports))
	for i, p := range ports {
		out[i] = p.Name
	}
	return out
}

func envMap(env []corev1.EnvVar) map[string]string {
	out := map[string]string{}
	for _, e := range env {
		out[e.Name] = e.Value
	}
	return out
}

func hasVolume(volumes []corev1.Volume, name string) bool {
	return slices.ContainsFunc(volumes, func(v corev1.Volume) bool { return v.Name == name })
}
