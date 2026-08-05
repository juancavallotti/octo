package kube

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"
)

// routePublisher publishes endpoints as Gateway API HTTPRoutes attached to a
// Gateway the cluster owner runs.
//
// It is markedly smaller than the Ingress publisher, and the difference is the
// point of the whole exercise: the route says only which host goes to which
// Service. Which proxy serves it, where its certificate comes from, and any
// controller-specific behaviour all belong to the Gateway's listener, which is
// somebody else's object — so none of it has to be handed to the orchestrator as
// configuration and stamped onto every object it creates.
type routePublisher struct {
	client    gatewayclient.Interface
	namespace string
	gateway   GatewayRef
}

func (p *routePublisher) publish(ctx context.Context, ep endpoint) error {
	_, err := p.client.GatewayV1().HTTPRoutes(p.namespace).Create(ctx, p.route(ep), metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create httproute: %w", err)
	}
	return nil
}

func (p *routePublisher) withdraw(ctx context.Context, name string) error {
	err := p.client.GatewayV1().HTTPRoutes(p.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err := ignoreNotFound(err); err != nil {
		return fmt.Errorf("delete httproute: %w", err)
	}
	return nil
}

// preflight fails when the cluster has no Gateway API. Unlike Ingress this is a
// set of CRDs, so "configured for Gateway API" and "the cluster has Gateway API"
// are genuinely independent — and without this the mismatch surfaces as the
// first exposed deploy failing, long after the install that chose the mode.
//
// It deliberately does not check that the Gateway itself exists. That object
// belongs to whoever runs the ingress infrastructure, its lifetime is not ours,
// and coupling the orchestrator's startup to it would take the whole platform
// down for the duration of somebody else's Gateway migration. A route that
// attaches to nothing reports it on its own status; the chart's install notes
// say where to look.
func (p *routePublisher) preflight(context.Context) error {
	gv := gatewayv1.GroupVersion.String()
	resources, err := p.client.Discovery().ServerResourcesForGroupVersion(gv)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("gateway API (%s) is not installed on this cluster: "+
				"install the Gateway API CRDs, or configure external endpoints for Ingress instead", gv)
		}
		return fmt.Errorf("discover %s: %w", gv, err)
	}
	for _, r := range resources.APIResources {
		if r.Kind == "HTTPRoute" {
			return nil
		}
	}
	return fmt.Errorf("cluster serves %s but not HTTPRoute: install the full Gateway API CRD bundle", gv)
}

// route builds the per-deployment HTTPRoute: hostname {subdomain}.{baseDomain}
// forwarded to the deployment's Service, attached to the configured Gateway.
//
// The Service is in the route's own namespace, so no ReferenceGrant is involved.
// A cross-namespace *parent* — a Gateway in the ingress team's namespace, which
// is the usual arrangement — needs no grant either; the Gateway's listener says
// which namespaces may attach to it, via allowedRoutes.
func (p *routePublisher) route(ep endpoint) *gatewayv1.HTTPRoute {
	parent := gatewayv1.ParentReference{Name: gatewayv1.ObjectName(p.gateway.Name)}
	// Namespace is left unset when the Gateway shares ours, which is what "the
	// route's own namespace" already means — setting it changes nothing and makes
	// the two cases render differently for no reason.
	if p.gateway.Namespace != "" && p.gateway.Namespace != p.namespace {
		ns := gatewayv1.Namespace(p.gateway.Namespace)
		parent.Namespace = &ns
	}
	// Unset attaches to every listener that will have us — which for a Gateway
	// with an HTTP and an HTTPS listener means both, and so a plaintext endpoint
	// alongside the intended one. Naming the listener is how an install says
	// "the HTTPS one".
	if p.gateway.SectionName != "" {
		sn := gatewayv1.SectionName(p.gateway.SectionName)
		parent.SectionName = &sn
	}

	port := gatewayv1.PortNumber(ep.port)
	pathPrefix := gatewayv1.PathMatchPathPrefix
	rootPath := "/"

	return &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ep.name,
			Labels: ep.labels,
		},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{parent},
			},
			Hostnames: []gatewayv1.Hostname{gatewayv1.Hostname(ep.host)},
			Rules: []gatewayv1.HTTPRouteRule{{
				// The CRD defaults matches to exactly this, but writing it out keeps
				// the route readable next to the Ingress it replaces, and keeps what
				// the orchestrator sends independent of a default it does not own.
				Matches: []gatewayv1.HTTPRouteMatch{{
					Path: &gatewayv1.HTTPPathMatch{Type: &pathPrefix, Value: &rootPath},
				}},
				BackendRefs: []gatewayv1.HTTPBackendRef{{
					BackendRef: gatewayv1.BackendRef{
						BackendObjectReference: gatewayv1.BackendObjectReference{
							Name: gatewayv1.ObjectName(ep.name),
							Port: &port,
						},
					},
				}},
			}},
		},
	}
}
