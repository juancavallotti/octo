// Package kv is the orchestrator's deployment-scoped, versioned key/value store. It
// backs the runtime's k8s services module: values are namespaced and use optimistic
// concurrency. Most values are stored as-is; values in a secret namespace (one with
// a "_secrets" suffix, e.g. system_secrets / user_secrets) are encrypted at rest
// with AES-GCM, so the secret store shares this one table without plain KV traffic
// paying any encryption cost. Reads transparently decrypt.
//
// A namespace also names a durability tier. A "_volatile" suffix (user_volatile,
// system_volatile) marks state whose loss is survivable — a memoized cache body
// rather than an in-flight aggregation — and routes to Redis instead of Postgres,
// so it costs neither a row nor a transaction and may be evicted under memory
// pressure. Runtime pods write those keys straight to Redis; what reaches this
// package for them is the object browser and undeploy cleanup. See redisrepo.go.
package kv

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
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

// Service stores values, routing each namespace to its tier's repo and encrypting
// those in a secret namespace before they reach it.
type Service struct {
	repo     repository
	volatile repository      // nil falls back to repo; see repoFor
	cipher   *cryptox.Cipher // nil disables secrets (secret-namespace ops fail with ErrEncryptionDisabled)
}

// NewService returns a service backed by repo for persistent namespaces and
// volatile for volatile ones.
//
// A nil volatile is not an error: volatile namespaces then land in repo alongside
// persistent ones. That is a worse deal — a database row for a value that expires
// in a minute — but not a broken one, and refusing writes because an optional
// dependency is absent would take the platform down over the one tier whose whole
// promise is that losing it is survivable. Pass NewRedisRepo(nil) for that.
//
// cipher may be nil to run without encryption configured, in which case reads and
// writes in a secret namespace fail with ErrEncryptionDisabled while plain
// namespaces still work.
func NewService(repo repository, volatile repository, cipher *cryptox.Cipher) *Service {
	if isNilRepo(volatile) {
		// A typed nil (*RedisRepo)(nil) is not == nil, and would panic on first use
		// rather than falling back. Normalize it here so callers can pass the result
		// of NewRedisRepo straight through.
		volatile = nil
	}
	return &Service{repo: repo, volatile: volatile, cipher: cipher}
}

// isNilRepo reports whether a repository value is nil, including a typed nil.
//
// The Kind check is not decoration: reflect's IsNil panics on a value whose kind
// cannot be nil, so a repository implemented as a struct value rather than a
// pointer would take the constructor down instead of being accepted.
func isNilRepo(repo repository) bool {
	if repo == nil {
		return true
	}
	v := reflect.ValueOf(repo)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return v.IsNil()
	default:
		return false
	}
}

// repoFor picks the store a namespace lives in.
//
//nolint:ireturn // returns the repository interface both tiers satisfy
func (s *Service) repoFor(namespace string) repository {
	if s.volatile != nil && isVolatile(namespace) {
		return s.volatile
	}
	return s.repo
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
	value, version, ok, err := s.repoFor(namespace).Get(ctx, deploymentID, namespace, key)
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
// browser uses it for the non-secret user namespaces, so no decryption is involved.
func (s *Service) List(ctx context.Context, deploymentID, namespace string) ([]Entry, error) {
	return s.repoFor(namespace).List(ctx, deploymentID, namespace)
}

// ListNamespaces returns the distinct namespaces holding data for a deployment,
// across both tiers: a namespace lives in one store or the other, so the browser
// would otherwise show only half of what a deployment has. Duplicates are collapsed
// in case a namespace somehow holds data in both.
func (s *Service) ListNamespaces(ctx context.Context, deploymentID string) ([]string, error) {
	persistent, err := s.repo.ListNamespaces(ctx, deploymentID)
	if err != nil {
		return nil, err
	}
	if s.volatile == nil {
		return persistent, nil
	}
	volatile, err := s.volatile.ListNamespaces(ctx, deploymentID)
	if err != nil {
		// Degrade rather than fail. The volatile tier is the one whose loss is
		// survivable, and a browser that showed nothing at all because Redis is down
		// would hide the persistent namespaces too — which are exactly what someone
		// wants to look at while a dependency is misbehaving. This matches how the
		// rest of this file treats the tier: an absent one is accepted at
		// construction, and undeploy carries on past a failure in either.
		slog.Warn("kv: listing volatile namespaces failed; reporting the persistent tier only",
			"deploymentId", deploymentID, "error", err)
		return persistent, nil
	}

	seen := make(map[string]struct{}, len(persistent)+len(volatile))
	out := make([]string, 0, len(persistent)+len(volatile))
	for _, namespace := range append(persistent, volatile...) {
		if _, dup := seen[namespace]; dup {
			continue
		}
		seen[namespace] = struct{}{}
		out = append(out, namespace)
	}
	return out, nil
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
	return s.repoFor(namespace).Write(ctx, deploymentID, namespace, key, value, expectedVersion)
}

// Delete removes a key (see Repo.Delete for the version semantics).
func (s *Service) Delete(ctx context.Context, deploymentID, namespace, key string, expectedVersion int64) error {
	return s.repoFor(namespace).Delete(ctx, deploymentID, namespace, key, expectedVersion)
}

// DeleteByDeployment removes every key for a deployment, from both tiers, for
// cleanup on undeploy.
//
// Both are attempted even when the first fails, and the errors are joined: these
// are two independent stores, and skipping the second because the first was
// unreachable would leave orphans nothing later goes back for.
func (s *Service) DeleteByDeployment(ctx context.Context, deploymentID string) error {
	err := s.repo.DeleteByDeployment(ctx, deploymentID)
	if s.volatile != nil {
		err = errors.Join(err, s.volatile.DeleteByDeployment(ctx, deploymentID))
	}
	return err
}
