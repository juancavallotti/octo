package secret

import "context"

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
	if !validName(name) {
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
