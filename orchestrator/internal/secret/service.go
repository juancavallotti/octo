package secret

import (
	"context"
	"log/slog"
)

// repository is the persistence surface the service needs. Declared in the
// consumer (and unexported) so service tests can substitute a fake; *Repo
// satisfies it structurally.
type repository interface {
	Upsert(ctx context.Context, name string) (Secret, error)
	List(ctx context.Context) ([]Secret, error)
	Delete(ctx context.Context, name string) error
}

// kubeSecrets is the Kubernetes surface the service drives to store and remove the
// actual values. *kube.Client satisfies it.
type kubeSecrets interface {
	SetSecret(ctx context.Context, name, value string) error
	DeleteSecretKey(ctx context.Context, name string) error
	// ListSecretNames returns the keys present in the shared Secret. Keys only —
	// no value ever leaves the cluster through this interface, which is what makes
	// the reconciler below safe to run unattended.
	ListSecretNames(ctx context.Context) ([]string, error)
}

// deploymentRefs reports whether a secret is still referenced by a deployment, so
// a delete can refuse to orphan a live workload's env. *deployment.Repo satisfies
// it.
type deploymentRefs interface {
	SecretReferenced(ctx context.Context, name string) (bool, error)
}

// Service holds cluster-secret lifecycle logic: it stores values in the shared
// Kubernetes Secret and records the catalog of names in the database.
type Service struct {
	repo        repository
	kube        kubeSecrets
	deployments deploymentRefs
}

// NewService returns a Service. kube may be nil, in which case all operations
// return ErrUnavailable (the caller should not register the routes then).
func NewService(repo repository, kube kubeSecrets, deployments deploymentRefs) *Service {
	return &Service{repo: repo, kube: kube, deployments: deployments}
}

// Create stores value under name (creating or overwriting), then records the name
// in the catalog. The value is written to Kubernetes only; the catalog never sees
// it. An invalid name is rejected before anything is written.
func (s *Service) Create(ctx context.Context, name, value string) (Secret, error) {
	if s.kube == nil {
		return Secret{}, ErrUnavailable
	}
	if !ValidName(name) {
		return Secret{}, ErrInvalidName
	}
	if err := s.kube.SetSecret(ctx, name, value); err != nil {
		return Secret{}, err
	}
	return s.repo.Upsert(ctx, name)
}

// List returns the catalog of secret names with their timestamps. It never returns
// values.
func (s *Service) List(ctx context.Context) ([]Secret, error) {
	return s.repo.List(ctx)
}

// Delete removes a secret's value and its catalog entry. Unless force is set, it
// refuses (ErrInUse) when a deployment still references the secret, since deleting
// it would break that workload on its next restart.
func (s *Service) Delete(ctx context.Context, name string, force bool) error {
	if s.kube == nil {
		return ErrUnavailable
	}
	if !force {
		used, err := s.deployments.SecretReferenced(ctx, name)
		if err != nil {
			return err
		}
		if used {
			return ErrInUse
		}
	}
	if err := s.kube.DeleteSecretKey(ctx, name); err != nil {
		return err
	}
	return s.repo.Delete(ctx, name)
}

// Reconcile drops catalogue entries for secrets the cluster does not have, and
// reports how many it dropped.
//
// The catalogue and the values are two writes to two systems — a row in
// cluster_secrets and a key in the shared octo-secrets Secret — so a cluster
// rebuilt from scratch leaves the platform listing secrets that are gone. A
// deployment binding one of those fails to start with a message about a missing
// key, while the secrets page insists it is there.
//
// **The other direction is deliberately left alone.** A key in the Secret with no
// catalogue row is logged and kept: deleting key material on the strength of a
// missing database row is not a trade worth making automatically, and the failure
// that would justify it — a catalogue restored from an older backup — is exactly
// the case where the key is the thing worth saving.
func (s *Service) Reconcile(ctx context.Context) (int, error) {
	if s.kube == nil {
		return 0, nil
	}

	names, err := s.kube.ListSecretNames(ctx)
	if err != nil {
		// Reported rather than repaired, for the same reason the deployment sweep
		// refuses to act on a failed listing: an empty answer from a cluster that was
		// never successfully asked is an instruction to delete the whole catalogue.
		return 0, err
	}
	inCluster := make(map[string]bool, len(names))
	for _, name := range names {
		inCluster[name] = true
	}

	rows, err := s.repo.List(ctx)
	if err != nil {
		return 0, err
	}

	dropped := 0
	for _, row := range rows {
		if inCluster[row.Name] {
			delete(inCluster, row.Name)
			continue
		}
		if err := s.repo.Delete(ctx, row.Name); err != nil {
			slog.Error("secret reconcile: drop catalogue entry", "name", row.Name, "error", err)
			continue
		}
		slog.Warn("secret reconcile: dropped a catalogue entry the cluster does not have",
			"name", row.Name)
		dropped++
	}

	for name := range inCluster {
		slog.Warn("secret reconcile: the cluster holds a secret the catalogue does not list; "+
			"it is being kept, not deleted", "name", name)
	}
	return dropped, nil
}
