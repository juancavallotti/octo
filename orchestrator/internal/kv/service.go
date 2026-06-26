// Package kv is the orchestrator's deployment-scoped, versioned key/value store. It
// backs the runtime's k8s services module: values are namespaced and use optimistic
// concurrency. Most values are stored as-is; values in a secret namespace (one with
// a "_secrets" suffix, e.g. system_secrets / user_secrets) are encrypted at rest
// with AES-GCM, so the secret store shares this one table without plain KV traffic
// paying any encryption cost. Reads transparently decrypt.
package kv

import (
	"context"
	"strings"
)

// secretNamespaceSuffix marks the namespaces whose values are encrypted at rest. It
// mirrors the runtime's core.NewSecretStore, which writes secrets to "<ns>_secrets".
const secretNamespaceSuffix = "_secrets"

// repository is the persistence surface the service needs; *Repo satisfies it.
// Declared in the consumer so service tests can substitute a fake.
type repository interface {
	Get(ctx context.Context, deploymentID, namespace, key string) ([]byte, int64, bool, error)
	Write(ctx context.Context, deploymentID, namespace, key string, value []byte, expectedVersion int64) (int64, error)
	Delete(ctx context.Context, deploymentID, namespace, key string, expectedVersion int64) error
	DeleteByDeployment(ctx context.Context, deploymentID string) error
}

// Service stores values, encrypting those in a secret namespace before they reach
// the repo and decrypting them on read.
type Service struct {
	repo   repository
	cipher *Cipher // nil disables secrets (secret-namespace ops fail with ErrEncryptionDisabled)
}

// NewService returns a service backed by repo. cipher may be nil to run without
// encryption configured, in which case writes/reads in a secret namespace fail with
// ErrEncryptionDisabled while plain namespaces still work.
func NewService(repo repository, cipher *Cipher) *Service {
	return &Service{repo: repo, cipher: cipher}
}

// isSecret reports whether a namespace holds encrypted-at-rest values.
func isSecret(namespace string) bool {
	return strings.HasSuffix(namespace, secretNamespaceSuffix)
}

// Get returns the value and version for a key, decrypting it when the namespace is a
// secret namespace. ok is false when absent.
func (s *Service) Get(ctx context.Context, deploymentID, namespace, key string) ([]byte, int64, bool, error) {
	value, version, ok, err := s.repo.Get(ctx, deploymentID, namespace, key)
	if err != nil || !ok {
		return nil, 0, ok, err
	}
	if isSecret(namespace) {
		if s.cipher == nil {
			return nil, 0, false, ErrEncryptionDisabled
		}
		value, err = s.cipher.Decrypt(value)
		if err != nil {
			return nil, 0, false, err
		}
	}
	return value, version, true, nil
}

// Set stores value, encrypting it first when the namespace is a secret namespace.
func (s *Service) Set(ctx context.Context, deploymentID, namespace, key string, value []byte, expectedVersion int64) (int64, error) {
	if isSecret(namespace) {
		if s.cipher == nil {
			return 0, ErrEncryptionDisabled
		}
		ciphertext, err := s.cipher.Encrypt(value)
		if err != nil {
			return 0, err
		}
		value = ciphertext
	}
	return s.repo.Write(ctx, deploymentID, namespace, key, value, expectedVersion)
}

// Delete removes a key (see Repo.Delete for the version semantics).
func (s *Service) Delete(ctx context.Context, deploymentID, namespace, key string, expectedVersion int64) error {
	return s.repo.Delete(ctx, deploymentID, namespace, key, expectedVersion)
}

// DeleteByDeployment removes every key for a deployment (both plain and secret
// namespaces live in the one table), for cleanup on undeploy.
func (s *Service) DeleteByDeployment(ctx context.Context, deploymentID string) error {
	return s.repo.DeleteByDeployment(ctx, deploymentID)
}
