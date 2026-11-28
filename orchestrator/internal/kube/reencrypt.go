package kube

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const (
	controllerTraefik = "traefik"
	controllerNginx   = "nginx"

	// traefikControllerName and nginxControllerName are the spec.controller values
	// the two controllers register their IngressClasses under.
	traefikControllerName = "traefik.io/ingress-controller"
	nginxControllerName   = "k8s.io/ingress-nginx"
	// defaultIngressClass marks the IngressClass that claims an Ingress naming none.
	defaultIngressClass = "ingressclass.kubernetes.io/is-default-class"

	traefikServersScheme    = "traefik.ingress.kubernetes.io/service.serversscheme"
	traefikServersTransport = "traefik.ingress.kubernetes.io/service.serverstransport"

	nginxBackendProtocol   = "nginx.ingress.kubernetes.io/backend-protocol"
	nginxProxySSLVerify    = "nginx.ingress.kubernetes.io/proxy-ssl-verify"
	nginxProxySSLSecret    = "nginx.ingress.kubernetes.io/proxy-ssl-secret"
	nginxProxySSLName      = "nginx.ingress.kubernetes.io/proxy-ssl-name"
	nginxProxySSLServerSNI = "nginx.ingress.kubernetes.io/proxy-ssl-server-name"
)

// serversTransportGVR is Traefik's ServersTransport, which tells Traefik how to
// verify a backend it reaches over HTTPS.
var serversTransportGVR = schema.GroupVersionResource{
	Group: "traefik.io", Version: "v1alpha1", Resource: "serverstransports",
}

// backendTLS is how an external endpoint verifies a deployment it reaches over
// HTTPS: the name on the pod's certificate, and where the CA that signed it is.
type backendTLS struct {
	// serverName is the host the pod's certificate is verified against.
	serverName string
	// caSecret is a Secret in the endpoint's namespace whose ca.crt verifies it.
	caSecret string
	// caConfigMap is a ConfigMap in the endpoint's namespace holding the CA under ca.crt.
	caConfigMap string
}

// backendTLSFor is the re-encrypt setup for an exposed deployment serving
// internal TLS, or nil when its endpoint reaches it over plain HTTP.
func (c *Client) backendTLSFor(ctx context.Context, spec Spec) *backendTLS {
	if !c.servesTLS(spec) {
		return nil
	}
	return &backendTLS{
		serverName:  serviceHost(spec.ID, c.ns(ctx)),
		caSecret:    internalTLSSecretName(spec.ID),
		caConfigMap: c.internalCAConfigMap,
	}
}

// ingressController names the controller behind the IngressClass this
// publisher's Ingresses are claimed by — the configured one, or the cluster's
// default when none is configured — as far as re-encrypting goes, or "" for one
// this orchestrator cannot configure. The class is read for its spec.controller
// because its name is whatever the cluster owner chose to call it.
func (p *ingressPublisher) ingressController(ctx context.Context) (string, error) {
	classes := p.clientset.NetworkingV1().IngressClasses()
	controller := ""
	if p.ingressClass != "" {
		class, err := classes.Get(ctx, p.ingressClass, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("read ingress class %q: %w", p.ingressClass, err)
		}
		controller = class.Spec.Controller
	} else {
		list, err := classes.List(ctx, metav1.ListOptions{})
		if err != nil {
			return "", fmt.Errorf("list ingress classes: %w", err)
		}
		for _, class := range list.Items {
			if class.Annotations[defaultIngressClass] == "true" {
				controller = class.Spec.Controller
				break
			}
		}
	}
	switch controller {
	case traefikControllerName:
		return controllerTraefik, nil
	case nginxControllerName:
		return controllerNginx, nil
	}
	return "", nil
}

// nginxReencryptAnnotations tell ingress-nginx to reach the backend over HTTPS
// and verify it against the CA in the deployment's certificate Secret.
func nginxReencryptAnnotations(namespace string, tls *backendTLS) map[string]string {
	return map[string]string{
		nginxBackendProtocol:   "HTTPS",
		nginxProxySSLVerify:    "on",
		nginxProxySSLSecret:    namespace + "/" + tls.caSecret,
		nginxProxySSLName:      tls.serverName,
		nginxProxySSLServerSNI: "on",
	}
}

// publishTraefikTransport creates the ServersTransport that verifies the pod, and
// points the deployment's Service at it, which is where Traefik reads both the
// backend scheme and the transport from.
func (p *ingressPublisher) publishTraefikTransport(ctx context.Context, namespace string, ep endpoint) error {
	transport := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "traefik.io/v1alpha1",
		"kind":       "ServersTransport",
		"metadata":   map[string]any{"name": ep.name, "labels": toAny(ep.labels)},
		"spec": map[string]any{
			"serverName": ep.tls.serverName,
			"rootCAs":    []any{map[string]any{"secret": ep.tls.caSecret}},
		},
	}}
	transport.SetOwnerReferences([]metav1.OwnerReference{ep.owner})
	_, err := p.dynamic.Resource(serversTransportGVR).Namespace(namespace).Create(ctx, transport, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create serverstransport: %w", err)
	}
	patch := fmt.Sprintf(`{"metadata":{"annotations":{%q:"https",%q:%q}}}`,
		traefikServersScheme, traefikServersTransport, namespace+"-"+ep.name+"@kubernetescrd")
	_, err = p.clientset.CoreV1().Services(namespace).Patch(ctx, ep.name, types.MergePatchType,
		[]byte(patch), metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("annotate service for re-encrypt: %w", err)
	}
	return nil
}

func (p *ingressPublisher) withdrawTraefikTransport(ctx context.Context, name string) error {
	err := p.dynamic.Resource(serversTransportGVR).Namespace(p.ns(ctx)).Delete(ctx, name, metav1.DeleteOptions{})
	if err := ignoreNotFound(err); err != nil {
		return fmt.Errorf("delete serverstransport: %w", err)
	}
	return nil
}

// backendTLSPolicy tells the Gateway to reach the deployment's Service over
// HTTPS on its https port and verify it against the internal CA bundle.
func backendTLSPolicy(ep endpoint) *gatewayv1.BackendTLSPolicy {
	section := gatewayv1.SectionName(httpsPortName)
	return &gatewayv1.BackendTLSPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: ep.name, Labels: ep.labels, OwnerReferences: []metav1.OwnerReference{ep.owner},
		},
		Spec: gatewayv1.BackendTLSPolicySpec{
			TargetRefs: []gatewayv1.LocalPolicyTargetReferenceWithSectionName{{
				LocalPolicyTargetReference: gatewayv1.LocalPolicyTargetReference{
					Group: "", Kind: "Service", Name: gatewayv1.ObjectName(ep.name),
				},
				SectionName: &section,
			}},
			Validation: gatewayv1.BackendTLSPolicyValidation{
				CACertificateRefs: []gatewayv1.LocalObjectReference{{
					Group: "", Kind: "ConfigMap", Name: gatewayv1.ObjectName(ep.tls.caConfigMap),
				}},
				Hostname: gatewayv1.PreciseHostname(ep.tls.serverName),
			},
		},
	}
}
