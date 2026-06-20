package kube

import (
	"context"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

// secretsName is the single, shared cluster-secrets Secret in the namespace. Its
// data keys are the cluster-wide secret names; pods reference a key via a
// secretKeyRef. Holding one Secret (rather than one per name) keeps the pool a
// single object to list and reference.
const secretsName = "octo-secrets"

// SetSecret stores value under key name in the shared cluster-secrets Secret,
// creating the Secret on first use and overwriting an existing key. The value is
// never logged. Concurrent writers are tolerated: the read-modify-write is retried
// on a conflict (another writer updated the Secret) or an AlreadyExists race (two
// first-time creates), so different keys set in parallel all land.
func (c *Client) SetSecret(ctx context.Context, name, value string) error {
	secrets := c.clientset.CoreV1().Secrets(c.namespace)
	retriable := func(err error) bool {
		return apierrors.IsConflict(err) || apierrors.IsAlreadyExists(err)
	}
	return retry.OnError(retry.DefaultRetry, retriable, func() error {
		sec, err := secrets.Get(ctx, secretsName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			_, cerr := secrets.Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:   secretsName,
					Labels: map[string]string{labelManagedBy: managedByValue},
				},
				Type:       corev1.SecretTypeOpaque,
				StringData: map[string]string{name: value},
			}, metav1.CreateOptions{})
			return cerr
		}
		if err != nil {
			return fmt.Errorf("kube: get secret: %w", err)
		}
		if sec.Data == nil {
			sec.Data = map[string][]byte{}
		}
		sec.Data[name] = []byte(value)
		_, uerr := secrets.Update(ctx, sec, metav1.UpdateOptions{})
		return uerr
	})
}

// DeleteSecretKey removes key name from the shared cluster-secrets Secret. A
// missing Secret or a missing key is a no-op. The read-modify-write is retried on
// a conflict so a concurrent set/delete does not lose the update.
func (c *Client) DeleteSecretKey(ctx context.Context, name string) error {
	secrets := c.clientset.CoreV1().Secrets(c.namespace)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		sec, err := secrets.Get(ctx, secretsName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("kube: get secret: %w", err)
		}
		if _, ok := sec.Data[name]; !ok {
			return nil
		}
		delete(sec.Data, name)
		_, uerr := secrets.Update(ctx, sec, metav1.UpdateOptions{})
		return uerr
	})
}

// SecretKeyExists reports whether key name is present in the shared cluster-secrets
// Secret. A missing Secret reads as "not present". It never returns the value.
func (c *Client) SecretKeyExists(ctx context.Context, name string) (bool, error) {
	sec, err := c.clientset.CoreV1().Secrets(c.namespace).Get(ctx, secretsName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("kube: get secret: %w", err)
	}
	_, ok := sec.Data[name]
	return ok, nil
}

// ListSecretNames returns the sorted keys of the shared cluster-secrets Secret. It
// reads only the keys, never the values, so it cannot leak a secret. A missing
// Secret yields an empty list.
func (c *Client) ListSecretNames(ctx context.Context) ([]string, error) {
	sec, err := c.clientset.CoreV1().Secrets(c.namespace).Get(ctx, secretsName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("kube: get secret: %w", err)
	}
	names := make([]string, 0, len(sec.Data))
	for k := range sec.Data {
		names = append(names, k)
	}
	sort.Strings(names)
	return names, nil
}
