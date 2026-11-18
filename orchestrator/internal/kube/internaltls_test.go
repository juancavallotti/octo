package kube

import (
	"context"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

// tlsClient builds a Client from cfg and returns its fakes, since the objects
// internal TLS writes are spread across all three.
func tlsClient(cfg Config) (*Client, *fake.Clientset, *gatewayfake.Clientset, func(string) *unstructured.Unstructured) {
	cs := fake.NewSimpleClientset()
	gw := gatewayfake.NewSimpleClientset()
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
	if len(svc.Spec.Ports) != 1 {
		t.Errorf("service ports = %v, want plain HTTP only", portNames(svc.Spec.Ports))
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
	if port := rt.Spec.Rules[0].BackendRefs[0].Port; port == nil || int32(*port) != serviceHTTPSPort {
		t.Errorf("route backend port = %v, want 443", port)
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
