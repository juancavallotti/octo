package kube

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// httpsPortName names the HTTPS port on a deployment's container and Services.
	httpsPortName = "https"
	// httpsPort is where a deployment serving internal TLS listens for HTTPS,
	// handed to the runtime as HTTPS_PORT (the runtime then opens no plain HTTP
	// port), and the https port on its Services, so its address names it the way
	// a plain one names its HTTP port.
	httpsPort int32 = 8443

	envHTTPSPort   = "HTTPS_PORT"
	envTLSCertFile = "OCTO_TLS_CERT_FILE"
	envTLSKeyFile  = "OCTO_TLS_KEY_FILE"
	// envSSLCertDir is where Go's crypto/x509 looks for trusted roots. Listing the
	// system directory first keeps every public CA trusted beside the internal one.
	envSSLCertDir = "SSL_CERT_DIR"
	systemCertDir = "/etc/ssl/certs"

	tlsVolume    = "internal-tls"
	tlsMountPath = "/var/run/octo/tls"
	caVolume     = "internal-ca"
	caMountPath  = "/var/run/octo/ca"
	// caKey is the key a CA bundle ConfigMap holds the certificate under.
	caKey = "ca.crt"
)

// certificateGVR is cert-manager's Certificate, written through the dynamic
// client so the orchestrator does not link cert-manager's API module.
var certificateGVR = schema.GroupVersionResource{Group: "cert-manager.io", Version: "v1", Resource: "certificates"}

// InternalTLSEnabled reports whether deployments can serve HTTPS inside the
// cluster: an internal CA issuer is configured.
func (c *Client) InternalTLSEnabled() bool { return c.internalTLSIssuer != "" }

// servesTLS reports whether spec's pods serve internal HTTPS: it asked to, it
// has an HTTP port to put TLS on, and the installation can issue certificates.
func (c *Client) servesTLS(spec Spec) bool {
	return spec.InternalTLS && spec.networked() && c.InternalTLSEnabled()
}

// trustsInternalCA reports whether pods mount the internal CA bundle as a trust
// root. Every pod does when the installation publishes one, so a deployment can
// call another's https:// address whether or not it serves TLS itself.
func (c *Client) trustsInternalCA() bool { return c.internalCAConfigMap != "" }

// internalTLSSecretName is the Secret cert-manager writes a deployment's internal
// certificate to.
func internalTLSSecretName(deploymentID string) string {
	return resourceName(deploymentID) + "-internal-tls"
}

// serviceHost is the in-cluster DNS name of a deployment's own Service, which is
// the name its certificate is verified against on the re-encrypted hop from an
// external endpoint.
func serviceHost(deploymentID, namespace string) string {
	return resourceName(deploymentID) + "." + namespace + ".svc"
}

// internalDNSNames are the names a deployment's certificate covers: its own
// Service and, when it has one, its stable internal Service, each in every form
// a caller may resolve.
func internalDNSNames(spec Spec, namespace string) []string {
	services := []string{resourceName(spec.ID)}
	if spec.Slug != "" {
		services = append(services, internalServiceName(spec.Slug))
	}
	names := make([]string, 0, len(services)*4)
	for _, svc := range services {
		names = append(names, svc, svc+"."+namespace, svc+"."+namespace+".svc",
			svc+"."+namespace+".svc.cluster.local")
	}
	return names
}

// ensureCertificate asks cert-manager for spec's internal certificate. An
// existing Certificate is left as it is: its names derive from the deployment's
// id and slug, which never change.
func (c *Client) ensureCertificate(ctx context.Context, spec Spec, labels map[string]string) error {
	namespace := c.ns(ctx)
	cert := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "cert-manager.io/v1",
		"kind":       "Certificate",
		"metadata": map[string]any{
			"name":   internalTLSSecretName(spec.ID),
			"labels": toAny(labels),
		},
		"spec": map[string]any{
			"secretName": internalTLSSecretName(spec.ID),
			"dnsNames":   toAnySlice(internalDNSNames(spec, namespace)),
			"privateKey": map[string]any{"algorithm": "ECDSA", "size": int64(256)},
			"usages":     []any{"server auth"},
			"issuerRef": map[string]any{
				"name":  c.internalTLSIssuer,
				"kind":  "ClusterIssuer",
				"group": "cert-manager.io",
			},
		},
	}}
	_, err := c.dynamic.Resource(certificateGVR).Namespace(namespace).Create(ctx, cert, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("kube: create internal certificate: %w", err)
	}
	return nil
}

// deleteCertificate removes a deployment's internal Certificate and the Secret
// cert-manager wrote, which it leaves behind on its own. Missing objects are not
// an error.
func (c *Client) deleteCertificate(ctx context.Context, deploymentID string) error {
	name := internalTLSSecretName(deploymentID)
	err := c.dynamic.Resource(certificateGVR).Namespace(c.ns(ctx)).Delete(ctx, name, metav1.DeleteOptions{})
	if err := ignoreNotFound(err); err != nil {
		return fmt.Errorf("kube: delete internal certificate: %w", err)
	}
	err = c.clientset.CoreV1().Secrets(c.ns(ctx)).Delete(ctx, name, metav1.DeleteOptions{})
	if err := ignoreNotFound(err); err != nil {
		return fmt.Errorf("kube: delete internal certificate secret: %w", err)
	}
	return nil
}

// servicePorts is a deployment Service's one port: HTTPS when the deployment
// serves internal TLS, plain HTTP otherwise.
func (c *Client) servicePorts(spec Spec) []corev1.ServicePort {
	if c.servesTLS(spec) {
		return []corev1.ServicePort{{
			Name:       httpsPortName,
			Port:       httpsPort,
			TargetPort: intstr.FromString(httpsPortName),
		}}
	}
	return []corev1.ServicePort{{
		Name:       "http",
		Port:       spec.port(),
		TargetPort: intstr.FromInt(int(spec.port())),
	}}
}

// tlsEnv is what tells the runtime to serve HTTPS, and with which certificate,
// plus the trust root every pod gets.
func (c *Client) tlsEnv(spec Spec) []corev1.EnvVar {
	var env []corev1.EnvVar
	if c.servesTLS(spec) {
		env = append(env,
			corev1.EnvVar{Name: envHTTPSPort, Value: fmt.Sprint(httpsPort)},
			corev1.EnvVar{Name: envTLSCertFile, Value: tlsMountPath + "/" + corev1.TLSCertKey},
			corev1.EnvVar{Name: envTLSKeyFile, Value: tlsMountPath + "/" + corev1.TLSPrivateKeyKey})
	}
	if c.trustsInternalCA() {
		env = append(env, corev1.EnvVar{Name: envSSLCertDir, Value: systemCertDir + ":" + caMountPath})
	}
	return env
}

func (c *Client) tlsMounts(spec Spec) []corev1.VolumeMount {
	var mounts []corev1.VolumeMount
	if c.servesTLS(spec) {
		mounts = append(mounts, corev1.VolumeMount{Name: tlsVolume, MountPath: tlsMountPath, ReadOnly: true})
	}
	if c.trustsInternalCA() {
		mounts = append(mounts, corev1.VolumeMount{Name: caVolume, MountPath: caMountPath, ReadOnly: true})
	}
	return mounts
}

// tlsVolumes backs tlsMounts. Both are readable by everyone in the pod for the
// same reason the token volume is: the runtime is not root, the files are.
//
// The CA bundle is optional: a namespace trust-manager has not reached yet starts
// its pods without the extra root rather than holding them pending.
func (c *Client) tlsVolumes(spec Spec) []corev1.Volume {
	mode := int32(0o444)
	var volumes []corev1.Volume
	if c.servesTLS(spec) {
		volumes = append(volumes, corev1.Volume{
			Name: tlsVolume,
			VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
				SecretName:  internalTLSSecretName(spec.ID),
				DefaultMode: &mode,
			}},
		})
	}
	if c.trustsInternalCA() {
		optional := true
		volumes = append(volumes, corev1.Volume{
			Name: caVolume,
			VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: c.internalCAConfigMap},
				Items:                []corev1.KeyToPath{{Key: caKey, Path: caKey}},
				DefaultMode:          &mode,
				Optional:             &optional,
			}},
		})
	}
	return volumes
}

// servingPort is the runtime container's one serving port, which the Services
// target by name: HTTPS when the deployment serves internal TLS, plain HTTP
// otherwise.
func (c *Client) servingPort(spec Spec) corev1.ContainerPort {
	if c.servesTLS(spec) {
		return corev1.ContainerPort{Name: httpsPortName, ContainerPort: httpsPort}
	}
	return corev1.ContainerPort{Name: "http", ContainerPort: spec.port()}
}

func toAny(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func toAnySlice(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}
