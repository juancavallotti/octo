// Package kv is the orchestrator's deployment-scoped, versioned key/value store. It
// backs the runtime's k8s services module: values are namespaced and use optimistic
// concurrency. Most values are stored as-is; values in a secret namespace (one with
// a "_secrets" suffix, e.g. system_secrets / user_secrets) are encrypted at rest
// with AES-GCM, so the secret store shares this one table without plain KV traffic
// paying any encryption cost. Reads transparently decrypt.
//
// A namespace also names a durability tier. A "_volatile" suffix (user_volatile,
// system_volatile) marks state whose loss is survivable — a memoized cache body
// rather than an in-flight aggregation — and is where a caller says it does not
// want a database row and a transaction for a value that expires in a minute.
// Today every namespace lands in Postgres regardless; the suffix is recognized
// here so the routing can move to Redis without the runtime, the routes or the
// repo changing shape.
package kv

import (
	"context"
	"strings"

	cryptox "github.com/juancavallotti/octo/orchestrator/internal/crypto"
)

// secretNamespaceSuffix marks the namespaces whose values are encrypted at rest. It
// mirrors the runtime's core.NewSecretStore, which writes secrets to "<ns>_secrets".
// volatileNamespaceSuffix mirrors the runtime's core.VolatileNamespace the same way.
//
// These are string suffixes on both sides rather than a shared constant because the
// runtime and the orchestrator are separate Go modules; the pairing is the same
// hand-synced arrangement as redisx and the trace subject name.
const (
	secretNamespaceSuffix   = "_secrets"
	volatileNamespaceSuffix = "_volatile"
)

// repository is the persistence surface the service needs; *Repo satisfies it.
// Declared in the consumer so service tests can substitute a fake.
type repository interface {
	Get(ctx context.Context, deploymentID, namespace, key string) ([]byte, int64, bool, error)
	List(ctx context.Context, deploymentID, namespace string) ([]Entry, error)
	ListNamespaces(ctx context.Context, deploymentID string) ([]string, error)
	Write(ctx context.Context, deploymentID, namespace, key string, value []byte, expectedVersion int64) (int64, error)
	Delete(ctx context.Context, deploymentID, namespace, key string, expectedVersion int64) error
	DeleteByDeployment(ctx context.Context, deploymentID string) error
}

// Service stores values, encrypting those in a secret namespace before they reach
// the repo and decrypting them on read.
type Service struct {
	repo   repository
	cipher *cryptox.Cipher // nil disables secrets (secret-namespace ops fail with ErrEncryptionDisabled)
}

// NewService returns a service backed by repo. cipher may be nil to run without
// encryption configured, in which case writes/reads in a secret namespace fail with
// ErrEncryptionDisabled while plain namespaces still work.
func NewService(repo repository, cipher *cryptox.Cipher) *Service {
	return &Service{repo: repo, cipher: cipher}
}

// isSecret reports whether a namespace holds encrypted-at-rest values.
func isSecret(namespace string) bool {
	return strings.HasSuffix(namespace, secretNamespaceSuffix)
}

// isVolatile reports whether a namespace holds values a backend may drop.
func isVolatile(namespace string) bool {
	return strings.HasSuffix(namespace, volatileNamespaceSuffix)
}

// checkTiers rejects the one namespace combination that must never exist. A secret
// is the definition of a value whose loss is not survivable, and the volatile
// backends neither promise to keep it nor encrypt it — so "user_secrets_volatile"
// would be a credential in a store that is allowed to evict it and holds it in the
// clear. Nothing in the runtime composes the two; this is what keeps a hand-written
// API call from doing so.
//
// The suffixes have to be checked one level deep rather than only at the end,
// because composing them puts one suffix behind the other: "user_secrets_volatile"
// does not end in "_secrets" and "user_volatile_secrets" does not end in
// "_volatile". Looking at the last suffix alone would wave through exactly the
// namespace this exists to refuse.
func checkTiers(namespace string) error {
	secret := isSecret(namespace) || isSecret(strings.TrimSuffix(namespace, volatileNamespaceSuffix))
	volatile := isVolatile(namespace) || isVolatile(strings.TrimSuffix(namespace, secretNamespaceSuffix))
	if secret && volatile {
		return ErrSecretNotVolatile
	}
	return nil
}

// Get returns the value and version for a key, decrypting it when the namespace is a
// secret namespace. ok is false when absent.
func (s *Service) Get(ctx context.Context, deploymentID, namespace, key string) ([]byte, int64, bool, error) {
	if err := checkTiers(namespace); err != nil {
		return nil, 0, false, err
	}
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

// List returns metadata for every key in a namespace (see Repo.List). The object
// browser uses it for the non-secret user namespace, so no decryption is involved.
func (s *Service) List(ctx context.Context, deploymentID, namespace string) ([]Entry, error) {
	return s.repo.List(ctx, deploymentID, namespace)
}

// ListNamespaces returns the distinct namespaces holding data for a deployment
// (see Repo.ListNamespaces). The object browser filters secret namespaces out.
func (s *Service) ListNamespaces(ctx context.Context, deploymentID string) ([]string, error) {
	return s.repo.ListNamespaces(ctx, deploymentID)
}

// Set stores value, encrypting it first when the namespace is a secret namespace.
func (s *Service) Set(ctx context.Context, deploymentID, namespace, key string, value []byte, expectedVersion int64) (int64, error) {
	if err := checkTiers(namespace); err != nil {
		return 0, err
	}
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
