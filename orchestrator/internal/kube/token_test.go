package kube

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// The deployment's own platform token: written to a Secret, mounted read-only,
// and pointed at by two env vars that only make sense together.

// tokenSpec is a minimal deployable spec carrying a token.
func tokenSpec(token string) Spec {
	return Spec{
		ID:            "dep-1",
		IntegrationID: "int-1",
		Name:          "billing",
		Definition:    "flows: []",
		Replicas:      1,
		Token:         token,
	}
}

func withIAM(t *testing.T) *Client {
	t.Helper()
	cfg := testConfig("apps.example.com")
	cfg.RuntimeServices = RuntimeServices{
		Module:          "k8s",
		OrchestratorURL: "http://octo-orchestrator:8080",
		IAMURL:          "http://octo-iam:8093",
	}
	return testClientFor(cfg)
}

func TestApplyWritesTheTokenToASecretAndMountsIt(t *testing.T) {
	c := withIAM(t)
	ctx := context.Background()
	spec := tokenSpec("a.b.c")

	if err := c.Apply(ctx, spec); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	secret, err := c.clientset.CoreV1().Secrets(testNamespace).
		Get(ctx, tokenSecretName(spec.ID), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("the token secret was not created: %v", err)
	}
	// StringData is write-only against a real API server, but the fake keeps it,
	// and either field carrying the token is the assertion that matters.
	if got := secret.StringData[tokenFileName]; got != "a.b.c" {
		t.Errorf("secret holds %q, want the minted token", got)
	}

	dep, err := c.clientset.AppsV1().Deployments(testNamespace).
		Get(ctx, resourceName(spec.ID), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	pod := dep.Spec.Template.Spec

	var volume *corev1.Volume
	for i := range pod.Volumes {
		if pod.Volumes[i].Name == tokenVolume {
			volume = &pod.Volumes[i]
		}
	}
	if volume == nil {
		t.Fatalf("no %q volume; volumes = %+v", tokenVolume, pod.Volumes)
	}
	if volume.Secret == nil || volume.Secret.SecretName != tokenSecretName(spec.ID) {
		t.Fatalf("%q is not backed by the token secret: %+v", tokenVolume, volume.VolumeSource)
	}
	// The runner images run as 65532 and secret files are owned by root, so an
	// owner-only mode hides the token from the process that has to present it —
	// and hides it silently, which is how this shipped the first time.
	if volume.Secret.DefaultMode == nil || *volume.Secret.DefaultMode != 0o444 {
		t.Errorf("token volume mode = %v, want 0444: the runtime does not run as root",
			volume.Secret.DefaultMode)
	}

	var mount *corev1.VolumeMount
	for i := range pod.Containers[0].VolumeMounts {
		if pod.Containers[0].VolumeMounts[i].Name == tokenVolume {
			mount = &pod.Containers[0].VolumeMounts[i]
		}
	}
	if mount == nil {
		t.Fatalf("the token is not mounted; mounts = %+v", pod.Containers[0].VolumeMounts)
	}
	if !mount.ReadOnly {
		t.Error("the token is mounted writable, and nothing in the pod may rewrite it")
	}

	env := envOf(pod.Containers[0])
	if got := env[envTokenFile]; got != tokenMountPath+"/"+tokenFileName {
		t.Errorf("%s = %q, want the mounted path", envTokenFile, got)
	}
	if got := env[envIAMURL]; got != "http://octo-iam:8093" {
		t.Errorf("%s = %q, want the iam address", envIAMURL, got)
	}
}

// The two env vars travel together. A pod told where its token is but not where
// to renew it would present the same one until it expired.
func TestApplyOmitsTheTokenEnvWithoutAnIAMAddress(t *testing.T) {
	c := testClientFor(testConfig("apps.example.com"))
	ctx := context.Background()

	if err := c.Apply(ctx, tokenSpec("a.b.c")); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	dep, err := c.clientset.AppsV1().Deployments(testNamespace).
		Get(ctx, resourceName("dep-1"), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	env := envOf(dep.Spec.Template.Spec.Containers[0])
	if _, ok := env[envTokenFile]; ok {
		t.Errorf("%s was set with no iam address to renew against", envTokenFile)
	}
}

// An install with no iam configured deploys exactly the pod it did before there
// were any tokens.
func TestApplyWithoutATokenCreatesNoSecretAndMountsNothing(t *testing.T) {
	c := withIAM(t)
	ctx := context.Background()

	if err := c.Apply(ctx, tokenSpec("")); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	_, err := c.clientset.CoreV1().Secrets(testNamespace).
		Get(ctx, tokenSecretName("dep-1"), metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		t.Errorf("a token secret exists for a deployment with no token (err = %v)", err)
	}
	dep, err := c.clientset.AppsV1().Deployments(testNamespace).
		Get(ctx, resourceName("dep-1"), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	for _, v := range dep.Spec.Template.Spec.Volumes {
		if v.Name == tokenVolume {
			t.Error("a token volume was mounted for a deployment with no token")
		}
	}
}

// A rollout re-mints, so the Secret has to be replaced rather than left holding
// the token the previous version was given.
func TestRolloutReplacesTheToken(t *testing.T) {
	c := withIAM(t)
	ctx := context.Background()
	spec := tokenSpec("first.token")
	if err := c.Apply(ctx, spec); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	spec.Token = "second.token"
	if err := c.Rollout(ctx, spec); err != nil {
		t.Fatalf("Rollout: %v", err)
	}

	secret, err := c.clientset.CoreV1().Secrets(testNamespace).
		Get(ctx, tokenSecretName(spec.ID), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get secret after rollout: %v", err)
	}
	if got := secret.StringData[tokenFileName]; got != "second.token" {
		t.Errorf("secret holds %q after the rollout, want the re-minted token", got)
	}
}

// Undeploy takes the credential with it. A machine token renews at any age, so
// one left behind would go on working long after the deployment was gone.
func TestDeleteRemovesTheTokenSecret(t *testing.T) {
	c := withIAM(t)
	ctx := context.Background()
	if err := c.Apply(ctx, tokenSpec("a.b.c")); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := c.Delete(ctx, "dep-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := c.clientset.CoreV1().Secrets(testNamespace).
		Get(ctx, tokenSecretName("dep-1"), metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		t.Errorf("the token secret outlived the deployment (err = %v)", err)
	}
}
