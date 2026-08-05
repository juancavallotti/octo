package kube

import (
	"context"
	"fmt"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// endpoint is everything a publisher needs to know about one deployment's
// external endpoint. It is deliberately not a Spec: a publisher has no business
// with replicas, env or definitions, and passing the whole Spec would invite it
// to reach for them.
type endpoint struct {
	name   string            // shared deterministic resource name for this deployment
	host   string            // fully-qualified external host ({subdomain}.{baseDomain})
	port   int32             // port of the deployment's Service, the routing backend
	labels map[string]string // deployment labels, so the object is found and cleaned up like the rest
}

// endpointPublisher publishes the external endpoint of one deployment — the
// object that makes {subdomain}.{baseDomain} reach its Service — and withdraws
// it again on undeploy.
//
// It is an interface because there are exactly two ways a cluster does this and
// they differ in kind, not in fields: an Ingress, or a Gateway API HTTPRoute
// attached to a Gateway the cluster owner runs. Everything else about a
// deployment is identical either way, so the difference is confined to one
// implementation each rather than spread as a mode check across Apply, Delete
// and the config. There will not be a third: those are the two APIs Kubernetes
// has for ingress traffic.
type endpointPublisher interface {
	// publish creates the endpoint object. It is called only for deployments that
	// asked to be exposed, and only when a base domain is configured.
	publish(ctx context.Context, ep endpoint) error
	// withdraw removes it, by the deployment's resource name. A missing object is
	// not an error: undeploy is retried, and a deployment that was never exposed
	// has nothing to remove.
	withdraw(ctx context.Context, name string) error
	// preflight verifies at startup that this cluster can serve the kind of object
	// this publisher creates, so a mismatch is one clear error on boot rather than
	// a deploy that reports success and is never reachable.
	preflight(ctx context.Context) error
}

// ingressPublisher publishes endpoints as networking.k8s.io Ingresses. It is the
// default, and the only one that works on a cluster with no Gateway API CRDs.
//
// Everything it needs beyond the endpoint itself is cluster configuration the
// Ingress has no other way to express — which controller should claim the
// object, where its certificate comes from, and whatever controller-specific
// annotations the deployment target requires. That list is the reason the
// Gateway API path exists: none of it is part of the route there.
type ingressPublisher struct {
	clientset kubernetes.Interface
	namespace string
	// clusterIssuer names a cert-manager ClusterIssuer for per-host TLS. Empty =
	// no annotation and no per-host cert.
	clusterIssuer string
	// wildcardTLSSecret, when set, is a pre-issued *.{baseDomain} TLS Secret every
	// Ingress references instead of issuing a per-host cert via clusterIssuer.
	wildcardTLSSecret string
	// ingressClass names the IngressClass each Ingress declares. Empty omits the
	// field entirely, letting the cluster's default IngressClass (if any) claim
	// it — the correct behavior on a cluster that isn't this project's own k3s
	// bootstrap.
	ingressClass string
	// extraAnnotations are merged onto every Ingress, underneath the cert-manager
	// cluster-issuer annotation (which always wins on key collision) — e.g.
	// controller-specific body-size or timeout annotations.
	extraAnnotations map[string]string
}

func (p *ingressPublisher) publish(ctx context.Context, ep endpoint) error {
	_, err := p.clientset.NetworkingV1().Ingresses(p.namespace).Create(ctx, p.ingress(ep), metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create ingress: %w", err)
	}
	return nil
}

func (p *ingressPublisher) withdraw(ctx context.Context, name string) error {
	err := p.clientset.NetworkingV1().Ingresses(p.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err := ignoreNotFound(err); err != nil {
		return fmt.Errorf("delete ingress: %w", err)
	}
	return nil
}

// preflight has nothing to check: networking.k8s.io/v1 is a built-in API group,
// present on every supported cluster. Whether a controller claims the Ingress is
// a different question, and not one an API call can answer — an unclaimed Ingress
// is created happily and simply never gets an address.
func (p *ingressPublisher) preflight(context.Context) error { return nil }

// ingress builds the per-deployment Ingress: host {subdomain}.{baseDomain} routed
// to the deployment's Service. TLS comes from the shared wildcard Secret when one
// is configured; otherwise, if a ClusterIssuer is set, cert-manager issues a
// per-host cert via its annotation; otherwise the Ingress carries no TLS at all
// (the cluster terminates TLS elsewhere, or this endpoint is plain HTTP).
func (p *ingressPublisher) ingress(ep endpoint) *networkingv1.Ingress {
	pathType := networkingv1.PathTypePrefix

	// Copy extraAnnotations rather than mutate the shared map, and let the
	// cert-manager annotation win on key collision.
	annotations := make(map[string]string, len(p.extraAnnotations)+1)
	for k, v := range p.extraAnnotations {
		annotations[k] = v
	}

	var tls []networkingv1.IngressTLS
	switch {
	case p.wildcardTLSSecret != "":
		// The wildcard cert already covers {subdomain}.{baseDomain}; reference its
		// Secret and add no cert-manager annotation so no per-host cert is issued.
		tls = []networkingv1.IngressTLS{{Hosts: []string{ep.host}, SecretName: p.wildcardTLSSecret}}
	case p.clusterIssuer != "":
		annotations["cert-manager.io/cluster-issuer"] = p.clusterIssuer
		tls = []networkingv1.IngressTLS{{Hosts: []string{ep.host}, SecretName: ep.name + "-tls"}}
	}
	if len(annotations) == 0 {
		annotations = nil
	}

	var ingressClassName *string
	if p.ingressClass != "" {
		ic := p.ingressClass
		ingressClassName = &ic
	}

	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        ep.name,
			Labels:      ep.labels,
			Annotations: annotations,
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: ingressClassName,
			TLS:              tls,
			Rules: []networkingv1.IngressRule{{
				Host: ep.host,
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{{
							Path:     "/",
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: ep.name,
									Port: networkingv1.ServiceBackendPort{Number: ep.port},
								},
							},
						}},
					},
				},
			}},
		},
	}
}
