package environment

import (
	"context"
	"errors"
	"log/slog"

	"github.com/juancavallotti/octo/orchestrator/internal/kube"
)

// repository is the persistence surface the service needs. Declared in the
// consumer (and unexported) so service tests can substitute a fake; *Repo
// satisfies it structurally.
type repository interface {
	Create(ctx context.Context, e Environment) (Environment, error)
	Get(ctx context.Context, name string) (Environment, error)
	List(ctx context.Context) ([]Environment, error)
	Update(ctx context.Context, name string, p Patch) (Environment, error)
	Delete(ctx context.Context, name string) error
}

// namespaces is the Kubernetes surface the service drives. *kube.Client
// satisfies it.
type namespaces interface {
	EnsureNamespace(ctx context.Context, environment, namespace string) error
	NamespaceInUse(ctx context.Context, namespace string) (bool, error)
	DeleteNamespace(ctx context.Context, environment, namespace string) error
}

// Service holds environment lifecycle logic: it records environments in the
// database and provisions and removes their namespaces.
type Service struct {
	repo repository
	kube namespaces
	// defaultNamespace is the release namespace, which the default environment
	// runs in and no other environment may claim.
	defaultNamespace string
}

// NewService returns a Service. kube may be nil, in which case environments can
// be listed but not created or deleted (ErrUnavailable).
func NewService(repo repository, kube namespaces, defaultNamespace string) *Service {
	return &Service{repo: repo, kube: kube, defaultNamespace: defaultNamespace}
}

// Enabled reports whether environments can be created and deleted here.
func (s *Service) Enabled() bool { return s.kube != nil }

// defaultEnvironment is the environment every installation has: the release
// namespace, under the reserved name.
func (s *Service) defaultEnvironment() Environment {
	return Environment{
		Name:        Default,
		Namespace:   s.defaultNamespace,
		DisplayName: "Default",
		IsDefault:   true,
	}
}

// List returns the default environment followed by every stored one.
func (s *Service) List(ctx context.Context) ([]Environment, error) {
	stored, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	return append([]Environment{s.defaultEnvironment()}, stored...), nil
}

// Get returns the environment named name. The default environment is answered
// without a database read.
func (s *Service) Get(ctx context.Context, name string) (Environment, error) {
	if name == Default {
		return s.defaultEnvironment(), nil
	}
	return s.repo.Get(ctx, name)
}

// Create records a new environment and provisions its namespace.
//
// The row is written first: its primary key and unique namespace are what make
// two concurrent creates of the same name or namespace collide, which a check
// followed by a provision could not. If provisioning then fails, the row is
// removed again so the name is free to retry. Whatever provisioning left in the
// cluster is labelled for this environment, so the retry adopts it.
func (s *Service) Create(ctx context.Context, in Input, createdBy string) (Environment, error) {
	if s.kube == nil {
		return Environment{}, ErrUnavailable
	}
	if !ValidName(in.Name) {
		return Environment{}, ErrInvalidName
	}
	namespace := in.Namespace
	if namespace == "" {
		namespace = "octo-" + in.Name
	}
	if !validNamespace(namespace) {
		return Environment{}, ErrInvalidNamespace
	}
	if namespace == s.defaultNamespace {
		return Environment{}, ErrNamespaceTaken
	}

	e := Environment{
		Name:        in.Name,
		Namespace:   namespace,
		DisplayName: in.DisplayName,
		Description: in.Description,
	}
	if createdBy != "" {
		e.CreatedBy = &createdBy
	}
	created, err := s.repo.Create(ctx, e)
	if err != nil {
		return Environment{}, err
	}

	if err := s.kube.EnsureNamespace(ctx, created.Name, created.Namespace); err != nil {
		// Context.WithoutCancel: a create abandoned by its caller must still give
		// the name back, or it stays taken by an environment with no namespace.
		if derr := s.repo.Delete(context.WithoutCancel(ctx), created.Name); derr != nil {
			slog.Error("environment create: remove the row after a failed provision",
				"environment", created.Name, "error", derr)
		}
		if errors.Is(err, kube.ErrNamespaceTaken) {
			return Environment{}, ErrNamespaceTaken
		}
		return Environment{}, err
	}
	return created, nil
}

// Update changes an environment's display name or description.
func (s *Service) Update(ctx context.Context, name string, p Patch) (Environment, error) {
	if name == Default {
		return Environment{}, ErrDefault
	}
	return s.repo.Update(ctx, name, p)
}

// Delete removes an environment and its namespace. It refuses (ErrInUse) while
// the namespace still runs a deployment, a test run or a dev run.
//
// A namespace that turns out not to be this environment's is left standing and
// the row removed anyway: the environment is what was asked to go, and the
// namespace was never octo's to delete.
func (s *Service) Delete(ctx context.Context, name string) error {
	if name == Default {
		return ErrDefault
	}
	if s.kube == nil {
		return ErrUnavailable
	}
	e, err := s.repo.Get(ctx, name)
	if err != nil {
		return err
	}
	used, err := s.kube.NamespaceInUse(ctx, e.Namespace)
	if err != nil {
		return err
	}
	if used {
		return ErrInUse
	}
	if err := s.kube.DeleteNamespace(ctx, e.Name, e.Namespace); err != nil {
		if !errors.Is(err, kube.ErrNamespaceTaken) {
			return err
		}
		slog.Warn("environment delete: the namespace is not labelled as this environment's; leaving it",
			"environment", e.Name, "namespace", e.Namespace)
	}
	return s.repo.Delete(ctx, e.Name)
}
